//go:build !windows

package main

import (
	"errors"

	p2p "sintet/internal/social"
)

type voiceAudio struct{}

func openVoiceAudio(bool) (*voiceAudio, error)           { return nil, errors.New("voice audio is unavailable") }
func (*voiceAudio) close()                               {}
func (*voiceAudio) record(*[p2p.VoiceSamples]int16) bool { return false }
func (*voiceAudio) play(*[2 * p2p.VoiceSamples]int16)    {}
