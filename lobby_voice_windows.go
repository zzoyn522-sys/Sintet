package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
	p2p "sintet/internal/social"
)

var voiceWinMM = windows.NewLazySystemDLL("winmm.dll")
var voiceInputProcedures = voiceProcedures("waveIn", "Open", "PrepareHeader", "AddBuffer", "Start", "Reset", "UnprepareHeader", "Close")
var voiceOutputProcedures = voiceProcedures("waveOut", "Open", "PrepareHeader", "Write", "Reset", "UnprepareHeader", "Close")

func voiceProcedures(prefix string, names ...string) map[string]*windows.LazyProc {
	procedures := make(map[string]*windows.LazyProc, len(names))
	for _, name := range names {
		procedures[name] = voiceWinMM.NewProc(prefix + name)
	}
	return procedures
}

type voiceWaveFormat struct {
	format, channels        uint16
	sampleRate, byteRate    uint32
	blockAlign, bits, extra uint16
}

type voiceWaveHeader struct {
	data             *int16
	length, recorded uint32
	user             uintptr
	flags, loops     uint32
	next             uintptr
	reserved         uintptr
}

type voiceWaveBuffer struct {
	header            voiceWaveHeader
	data              [2 * p2p.VoiceSamples]int16
	prepared, playing bool
}

type voiceAudio struct {
	handle     uintptr
	procedures map[string]*windows.LazyProc
	buffers    [4]voiceWaveBuffer
	nextInput  int
	pin        runtime.Pinner
}

func openVoiceAudio(input bool) (*voiceAudio, error) {
	device := &voiceAudio{procedures: voiceOutputProcedures}
	if input {
		device.procedures = voiceInputProcedures
	}
	channels := uint16(2)
	if input {
		channels = 1
	}
	format := voiceWaveFormat{format: 1, channels: channels, sampleRate: p2p.VoiceSampleRate,
		byteRate: p2p.VoiceSampleRate * uint32(channels) * 2, blockAlign: channels * 2, bits: 16}
	err := device.call("Open", uintptr(unsafe.Pointer(&device.handle)), 0xffffffff, uintptr(unsafe.Pointer(&format)), 0, 0, 0)
	runtime.KeepAlive(&format)
	if err != nil {
		return nil, err
	}
	for i := range device.buffers {
		buffer := &device.buffers[i]
		device.pin.Pin(&buffer.data)
		device.pin.Pin(&buffer.header)
		buffer.header.data = &buffer.data[0]
		buffer.header.length = p2p.VoiceSamples * uint32(format.blockAlign)
		if err = device.headerCall("PrepareHeader", buffer); err != nil {
			device.close()
			return nil, err
		}
		buffer.prepared = true
		if input {
			if err = device.headerCall("AddBuffer", buffer); err != nil {
				device.close()
				return nil, err
			}
		}
	}
	if input {
		if err = device.call("Start", device.handle); err != nil {
			device.close()
			return nil, err
		}
	}
	return device, nil
}

//go:uintptrescapes
func (a *voiceAudio) call(method string, arguments ...uintptr) error {
	code, _, _ := a.procedures[method].Call(arguments...)
	if code != 0 {
		return fmt.Errorf("WinMM %s: %d", method, code)
	}
	return nil
}

func (a *voiceAudio) headerCall(method string, buffer *voiceWaveBuffer) error {
	return a.call(method, a.handle, uintptr(unsafe.Pointer(&buffer.header)), unsafe.Sizeof(buffer.header))
}

func (a *voiceAudio) close() {
	_ = a.call("Reset", a.handle)
	for i := range a.buffers {
		buffer := &a.buffers[i]
		if buffer.prepared {
			_ = a.headerCall("UnprepareHeader", buffer)
		}
	}
	_ = a.call("Close", a.handle)
	a.pin.Unpin()
}

const voiceBufferDone = 1

func (a *voiceAudio) record(frame *[p2p.VoiceSamples]int16) bool {
	buffer := &a.buffers[a.nextInput]
	if atomic.LoadUint32(&buffer.header.flags)&voiceBufferDone == 0 {
		return false
	}
	full := buffer.header.recorded == p2p.VoiceSamples*2
	if full {
		copy(frame[:], buffer.data[:p2p.VoiceSamples])
	}
	_ = a.headerCall("AddBuffer", buffer)
	a.nextInput = (a.nextInput + 1) % len(a.buffers)
	return full
}

func (a *voiceAudio) play(frame *[2 * p2p.VoiceSamples]int16) {
	for i := range a.buffers {
		buffer := &a.buffers[i]
		if !buffer.playing || atomic.LoadUint32(&buffer.header.flags)&voiceBufferDone != 0 {
			buffer.data = *frame
			buffer.playing = a.headerCall("Write", buffer) == nil
			return
		}
	}
}
