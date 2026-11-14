package localstore

import (
	"strings"
	"testing"
	"time"
)

func TestFutureClockSkewMessage(t *testing.T) {
	d := mkDraft("drf_00000000000000000001", 1, minimal("Synthetic"))
	d.CreatedAt = time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	d.UpdatedAt = d.CreatedAt
	if err := checkEnvelope(d); err == nil || !strings.Contains(err.Error(), "ahead of this machine") {
		t.Fatalf("write skew diagnostic: %v", err)
	}
	raw, err := encode(d)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decode(raw, d.ID)
	if err == nil || !strings.Contains(err.Error(), "ahead of this machine") {
		t.Fatalf("read skew diagnostic: %v", err)
	}
}
