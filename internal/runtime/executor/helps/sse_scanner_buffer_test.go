package helps

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestBorrowSSEScannerBufferScansAndGrows(t *testing.T) {
	small := bufio.NewScanner(strings.NewReader("data: {\"type\":\"response.output_text.delta\"}\n"))
	releaseSmall := BorrowSSEScannerBuffer(small, SSEScannerMaxTokenSize)
	if !small.Scan() {
		t.Fatalf("scan small line: %v", small.Err())
	}
	if !strings.Contains(small.Text(), "response.output_text.delta") {
		t.Fatalf("unexpected token %q", small.Text())
	}
	releaseSmall()
	releaseSmall()

	line := "data: " + strings.Repeat("x", SSEScannerBufSize+8) + "\n"
	large := bufio.NewScanner(strings.NewReader(line))
	releaseLarge := BorrowSSEScannerBuffer(large, SSEScannerMaxTokenSize)
	defer releaseLarge()
	if !large.Scan() {
		t.Fatalf("scan line larger than the pooled buffer: %v", large.Err())
	}
	if len(large.Bytes()) < SSEScannerBufSize {
		t.Fatalf("token length = %d, want > %d", len(large.Bytes()), SSEScannerBufSize)
	}
}

func TestBorrowSSEScannerBufferReuseOverwritesUnclonedBytes(t *testing.T) {
	const token = `data: {"id":"first"}`
	var alias, cloned []byte
	overwritten := false
	for attempt := 0; attempt < 16; attempt++ {
		alias, cloned = scanPooledToken(t, token+"\n")
		next := bufio.NewScanner(strings.NewReader("data: {\"id\":\"second-value\"}\n"))
		releaseNext := BorrowSSEScannerBuffer(next, SSEScannerMaxTokenSize)
		if !next.Scan() {
			releaseNext()
			t.Fatalf("attempt %d: scan reused buffer: %v", attempt, next.Err())
		}
		same := bytes.Equal(alias, cloned)
		releaseNext()
		if !same {
			overwritten = true
			break
		}
	}
	if !overwritten {
		t.Fatal("expected pooled buffer reuse to overwrite an uncloned scanner.Bytes slice")
	}
	if string(cloned) != token {
		t.Fatalf("clone = %q, want %q", cloned, token)
	}
}

func TestBorrowSSEScannerBufferHonorsSmallMaxTokenSize(t *testing.T) {
	const maxToken = 32
	short := bufio.NewScanner(strings.NewReader("data: hi\n"))
	releaseShort := BorrowSSEScannerBuffer(short, maxToken)
	defer releaseShort()
	if !short.Scan() {
		t.Fatalf("scan short line: %v", short.Err())
	}

	long := bufio.NewScanner(strings.NewReader(strings.Repeat("a", maxToken+8) + "\n"))
	releaseLong := BorrowSSEScannerBuffer(long, maxToken)
	defer releaseLong()
	if long.Scan() {
		t.Fatalf("expected ErrTooLong, scanned %d bytes", len(long.Bytes()))
	}
	if !errors.Is(long.Err(), bufio.ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong", long.Err())
	}
}

func scanPooledToken(t *testing.T, input string) (alias, cloned []byte) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(input))
	release := BorrowSSEScannerBuffer(scanner, SSEScannerMaxTokenSize)
	if !scanner.Scan() {
		release()
		t.Fatalf("scan pooled token: %v", scanner.Err())
	}
	alias = scanner.Bytes()
	cloned = bytes.Clone(alias)
	release()
	return alias, cloned
}
