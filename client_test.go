package radius

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_Exchange_expired(t *testing.T) {
	handler := HandlerFunc(func(w ResponseWriter, r *Request) {
		// ignore
	})
	server := NewTestServer(handler, StaticSecretSource([]byte(`12345`)))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), -time.Hour)
	defer cancel()

	req := New(CodeAccessRequest, []byte(`secret`))

	client := Client{}
	resp, err := client.Exchange(ctx, req, server.Addr)
	if resp != nil {
		t.Fatalf("got non-nil response (%v); expected nil", resp)
	}
	if err == nil {
		t.Fatal("got nil error; expected one")
	}
	if netErr, ok := err.(net.Error); !ok {
		t.Fatal("err is not a net.Error")
	} else if !netErr.Timeout() {
		t.Fatal("got netErr.Timeout() = false; expected true")
	}
}

func TestClient_Exchange_retry(t *testing.T) {
	secret := []byte(`12345`)

	var attempts int32

	handler := HandlerFunc(func(w ResponseWriter, r *Request) {
		if atomic.AddInt32(&attempts, 1) == 4 {
			w.Write(r.Response(CodeAccessAccept))
		}
	})
	server := NewTestServer(handler, StaticSecretSource(secret))
	defer server.Close()

	req := New(CodeAccessRequest, secret)

	client := Client{
		Retry: time.Millisecond * 5,
	}
	resp, err := client.Exchange(context.Background(), req, server.Addr)
	if err != nil {
		t.Fatalf("got err %s; expected nil", err)
	}
	if resp.Code != CodeAccessAccept {
		t.Fatalf("got code %s; expected %s", resp.Code, CodeAccessAccept)
	}
	if attempts := atomic.LoadInt32(&attempts); attempts != 4 {
		t.Fatalf("response received in %d attemps; expecting 4", attempts)
	}
}

func TestClient_Exchange_cancelled(t *testing.T) {
	secret := []byte(`12345`)
	handler := HandlerFunc(func(w ResponseWriter, r *Request) {
		// ignore
	})
	server := NewTestServer(handler, StaticSecretSource(secret))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond * 50)
		cancel()
	}()

	req := New(CodeAccessRequest, secret)

	client := Client{
		Retry: time.Millisecond * 5,
	}
	resp, err := client.Exchange(ctx, req, server.Addr)
	if resp != nil {
		t.Fatalf("got non-nil response (%v); expected nil", resp)
	}
	if err != context.Canceled {
		t.Fatalf("got error = %v; expecting context.Canceled", err)
	}
}

func TestClient_Exchange_invalidPacket(t *testing.T) {
	secret := []byte(`12345`)

	var server *TestServer
	handler := HandlerFunc(func(w ResponseWriter, r *Request) {
		// write bad data to client
		server.l.WriteTo([]byte(`AAAA`), r.RemoteAddr)
	})
	server = NewTestServer(handler, StaticSecretSource(secret))
	defer server.Close()

	req := New(CodeAccessRequest, secret)

	client := Client{
		Retry:           time.Millisecond * 5,
		MaxPacketErrors: 2,
	}
	resp, err := client.Exchange(context.Background(), req, server.Addr)
	if resp != nil {
		t.Fatalf("got non-nil response (%v); expected nil", resp)
	}
	if expecting := `packet not at least 20 bytes long`; !strings.Contains(err.Error(), expecting) {
		t.Fatalf("got error = %v; expecting %s", err, expecting)
	}
}

func TestClient_Exchange_nonauthenticPacket(t *testing.T) {
	secret := []byte(`12345`)

	var server *TestServer
	handler := HandlerFunc(func(w ResponseWriter, r *Request) {
		resp := r.Response(CodeAccessAccept)
		resp.Authenticator = [16]byte{}
		w.Write(resp)
	})
	server = NewTestServer(handler, StaticSecretSource(secret))
	defer server.Close()

	req := New(CodeAccessRequest, secret)

	client := Client{
		Retry:           time.Millisecond * 5,
		MaxPacketErrors: 2,
	}
	resp, err := client.Exchange(context.Background(), req, server.Addr)
	if resp != nil {
		t.Fatalf("got non-nil response (%v); expected nil", resp)
	}
	if _, ok := err.(*NonAuthenticResponseError); !ok {
		t.Fatalf("got error %T; expecting NonAuthenticResponseError", err)
	}
}
