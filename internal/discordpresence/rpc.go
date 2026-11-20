package discordpresence

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	handshake = iota
	frame
	closeFrame
	ping
	pong

	maxPayload = 64 * 1024
)

type Activity struct {
	Details    string
	State      string
	Started    int64
	LargeImage string
	PartyID    string
	Players    int
	Capacity   int
	JoinSecret string
}

const joinEvent = "ACTIVITY_JOIN"

type message struct {
	Cmd   string `json:"cmd"`
	Event string `json:"evt"`
	Nonce string `json:"nonce"`
	Data  struct {
		Secret string `json:"secret"`
	} `json:"data"`
}

type packet struct {
	opcode uint32
	body   []byte
}

type connection struct {
	net.Conn
	nonce uint64
}

func ValidApplicationID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id
}

// Run mirrors the latest activity to the local Discord client. Returning false
// from sample clears the status, including when sharing is disabled mid-session.
func Run(ctx context.Context, applicationID string, sample func() (Activity, bool), onJoin func(string)) {
	if !ValidApplicationID(applicationID) {
		return
	}
	for ctx.Err() == nil {
		if _, enabled := sample(); enabled || onJoin != nil {
			if pipe, err := dial(ctx); err == nil {
				session := connection{Conn: pipe}
				if session.open(applicationID) == nil {
					session.run(ctx, sample, onJoin)
				}
				_ = pipe.Close()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *connection) open(id string) error {
	if err := c.writeJSON(handshake, map[string]any{"v": 1, "client_id": id}); err != nil {
		return err
	}
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	for {
		p, err := readPacket(c)
		if err != nil {
			return err
		}
		switch p.opcode {
		case frame:
			var msg message
			if json.Unmarshal(p.body, &msg) == nil && msg.Cmd == "DISPATCH" && msg.Event == "READY" {
				return c.SetReadDeadline(time.Time{})
			}
		case ping:
			if err = c.write(pong, p.body); err != nil {
				return err
			}
		case closeFrame:
			return io.EOF
		}
	}
}

func (c *connection) run(ctx context.Context, sample func() (Activity, bool), onJoin func(string)) {
	if onJoin != nil {
		if err := c.writeJSON(frame, map[string]any{"cmd": "SUBSCRIBE", "evt": joinEvent, "nonce": c.nextNonce()}); err != nil {
			return
		}
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	done := make(chan struct{})
	defer close(done)
	incoming := make(chan packet)
	go func() {
		defer close(incoming)
		for {
			p, err := readPacket(c)
			if err != nil {
				return
			}
			select {
			case incoming <- p:
			case <-done:
				return
			}
		}
	}()
	var last Activity
	var sent bool
	var next time.Time
	var rejectedImage string
	defer func() {
		if sent {
			_ = c.publish(nil)
		}
	}()
	for ctx.Err() == nil {
		desired, enabled := sample()
		if !enabled && sent {
			sent = false
			if c.publish(nil) != nil {
				return
			}
			next = time.Time{}
		}
		if !enabled && onJoin == nil {
			return
		}
		if desired.LargeImage == rejectedImage {
			desired.LargeImage = ""
		}
		if enabled && (!sent || desired != last) && !time.Now().Before(next) {
			if c.publish(&desired) != nil {
				return
			}
			last, sent, next = desired, true, time.Now().Add(4*time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case p, ok := <-incoming:
			if !ok || p.opcode == closeFrame {
				sent = false
				return
			}
			if p.opcode == ping && c.write(pong, p.body) != nil {
				return
			}
			if p.opcode == frame {
				var reply message
				if json.Unmarshal(p.body, &reply) != nil {
					continue
				}
				if onJoin != nil && reply.Cmd == "DISPATCH" && reply.Event == joinEvent && reply.Data.Secret != "" {
					onJoin(reply.Data.Secret)
				}
				if sent && last.LargeImage != "" && reply.Cmd == "SET_ACTIVITY" &&
					reply.Event == "ERROR" && reply.Nonce == strconv.FormatUint(c.nonce, 10) {
					// Keep the activity visible if the configured artwork is unavailable.
					rejectedImage = last.LargeImage
					next = time.Time{}
				}
			}
		}
	}
}

func (c *connection) publish(activity *Activity) error {
	var data map[string]any
	if activity != nil {
		data = map[string]any{"details": activity.Details, "state": activity.State, "type": 0}
		if activity.Started != 0 {
			data["timestamps"] = map[string]int64{"start": activity.Started}
		}
		if activity.PartyID != "" {
			privacy := 0
			if activity.JoinSecret != "" {
				privacy = 1
				data["secrets"] = map[string]string{"join": activity.JoinSecret}
			}
			data["party"] = map[string]any{"id": activity.PartyID, "size": [2]int{activity.Players, activity.Capacity}, "privacy": privacy}
		}
		if activity.LargeImage != "" {
			data["assets"] = map[string]string{"large_image": activity.LargeImage, "large_text": "Sintet"}
		}
	}
	return c.writeJSON(frame, map[string]any{
		"cmd": "SET_ACTIVITY", "args": map[string]any{"pid": os.Getpid(), "activity": data},
		"nonce": c.nextNonce(),
	})
}

func (c *connection) nextNonce() string {
	c.nonce++
	return strconv.FormatUint(c.nonce, 10)
}

func (c *connection) writeJSON(opcode uint32, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.write(opcode, data)
}

func (c *connection) write(opcode uint32, body []byte) error {
	if err := c.SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return err
	}
	data := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(data, opcode)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(body)))
	copy(data[8:], body)
	n, err := c.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func readPacket(reader io.Reader) (packet, error) {
	var header [8]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return packet{}, err
	}
	size := binary.LittleEndian.Uint32(header[4:])
	if size > maxPayload {
		return packet{}, errors.New("Discord IPC frame exceeds the allowed size")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(reader, body); err != nil {
		return packet{}, err
	}
	return packet{opcode: binary.LittleEndian.Uint32(header[:]), body: body}, nil
}
