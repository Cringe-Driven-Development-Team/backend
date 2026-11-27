package environmentsrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
)

// TestDownloadWithholdsUncheckedTail доказывает, что обрывы и неверный digest не дают полный HTTP Content-Length.
func TestDownloadWithholdsUncheckedTail(t *testing.T) {
	full := []byte("headtail")
	hash := sha256.Sum256(full)
	valid := hex.EncodeToString(hash[:])
	for _, test := range []struct {
		name, hash string
		extra      bool
		want       string
	}{
		{"valid", valid, false, "headtail"},
		{"bad digest", "bad-hash", false, "head"},
		{"unexpected trailing frame", valid, true, "head"},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := []*pb.DownloadChunk{{Payload: []byte("head")}, {Offset: 4, Payload: []byte("tail")},
				{Offset: 8, Complete: true, Sha256: &test.hash}}
			if test.extra {
				frames = append(frames, &pb.DownloadChunk{Complete: true})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := &downloadReader{ctx: ctx, cancel: cancel, digest: sha256.New(), left: 8, recv: func() (*pb.DownloadChunk, error) {
				if len(frames) == 0 {
					return nil, io.EOF
				}
				v := frames[0]
				frames = frames[1:]
				return v, nil
			}}
			got, err := io.ReadAll(reader)
			if !bytes.Equal(got, []byte(test.want)) {
				t.Fatalf("bytes=%q err=%v", got, err)
			}
			if test.want == string(full) && err != nil || test.want != string(full) && !errors.Is(err, D.ErrProtocol) {
				t.Fatal(err)
			}
		})
	}
}
