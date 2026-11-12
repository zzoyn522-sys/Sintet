package social

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestLobbyTransfersBeforeWorldAndFollowsLANPortChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	host, err := NewPeer(-1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	host.SetPackHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lobby" {
			io.Copy(w, r.Body)
		} else {
			io.WriteString(w, "pack-before-world")
		}
	}))
	guest, err := NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	offer, err := guest.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.Answer(ctx, *offer)
	if err != nil {
		t.Fatal(err)
	}
	if err = guest.SetAnswer(*answer); err != nil {
		t.Fatal(err)
	}
	port, err := guest.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkPack := func() {
		t.Helper()
		r, e := guest.PackGet(ctx, "/manifest")
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r.Body)
		r.Body.Close()
		if e != nil || string(data) != "pack-before-world" {
			t.Fatal("file channel failed", e, string(data))
		}
	}
	checkPack()
	r, err := guest.LobbyExchange(ctx, []byte(`{"phase":"ready"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil || string(data) != `{"phase":"ready"}` {
		t.Fatal("lobby exchange failed", err, string(data))
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	probe := func(expect string) {
		t.Helper()
		conn, e := net.DialTimeout("tcp4", endpoint, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		conn.Write([]byte("hello"))
		buffer := make([]byte, 32)
		n, e := conn.Read(buffer)
		if expect == "" {
			if n > 0 {
				t.Fatal("connected to a world before it was opened")
			}
			return
		}
		if e != nil || string(buffer[:n]) != expect {
			t.Fatal("wrong Minecraft endpoint", e, string(buffer[:n]))
		}
	}
	probe("")
	for _, name := range []string{"world-one", "world-two"} {
		target, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			c, e := target.Accept()
			if e == nil {
				defer c.Close()
				buffer := make([]byte, 5)
				io.ReadFull(c, buffer)
				io.WriteString(c, name)
			}
		}()
		host.SetHostPort(target.Addr().(*net.TCPAddr).Port)
		probe(name)
		target.Close()
		host.SetHostPort(0)
		probe("")
		checkPack()
		if !guest.Connected() {
			t.Fatal("closing the world destroyed the lobby")
		}
	}
}
