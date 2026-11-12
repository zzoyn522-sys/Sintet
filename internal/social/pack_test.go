package social

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestPackChannelAndMinecraftTunnelRemainIndependent(t *testing.T) {
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	host, err := NewPeer(target.Addr().(*net.TCPAddr).Port, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	guest, err := NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	data := bytes.Repeat([]byte("encrypted pack payload"), 20000)
	host.SetPackHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			w.Write([]byte("manifest fixture"))
		} else {
			w.Write(data)
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
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
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := guest.PackGet(ctx, "/file/"+string(bytes.Repeat([]byte("a"), 64)))
			if e != nil {
				t.Error(e)
				return
			}
			got, e := io.ReadAll(r.Body)
			r.Body.Close()
			if e != nil || !bytes.Equal(got, data) {
				t.Error("pack data corrupted", e)
			}
		}()
	}
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write([]byte("world"))
	got := make([]byte, 5)
	if _, err = io.ReadFull(conn, got); err != nil || string(got) != "world" {
		t.Fatal("Minecraft stream mixed with file transfer", err)
	}
	wg.Wait()
	host.Close()
	if _, err = guest.PackGet(ctx, "/manifest"); err == nil {
		t.Fatal("closed peer still served files")
	}
}
