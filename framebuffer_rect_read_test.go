package rdp

import (
	"bytes"
	"testing"
)

func newRectReadClient(w, h int) *Client {
	c := &Client{}
	c.framebuf = NewFramebuffer(w, h)
	for i := range c.framebuf.Pixels {
		c.framebuf.Pixels[i] = byte(i * 7)
	}
	return c
}

func TestFramebufferWriteRectTopDownMatchesFullCopy(t *testing.T) {
	c := newRectReadClient(9, 7)
	full := make([]byte, 9*7*4)
	if w, h := c.FramebufferWriteTopDown(full); w != 9 || h != 7 {
		t.Fatalf("full copy dims = %dx%d", w, h)
	}
	cases := []struct{ x, y, w, h int }{
		{0, 0, 9, 7}, {2, 1, 3, 4}, {8, 6, 1, 1}, {0, 6, 9, 1}, {4, 0, 5, 7},
	}
	for _, tc := range cases {
		want := make([]byte, 0, tc.w*tc.h*4)
		for row := tc.y; row < tc.y+tc.h; row++ {
			off := row*9*4 + tc.x*4
			want = append(want, full[off:off+tc.w*4]...)
		}
		dst := make([]byte, tc.w*tc.h*4)
		fw, fh := c.FramebufferWriteRectTopDown(dst, tc.x, tc.y, tc.w, tc.h)
		if fw != 9 || fh != 7 {
			t.Fatalf("rect %+v: dims = %dx%d, want 9x7", tc, fw, fh)
		}
		if !bytes.Equal(dst, want) {
			t.Fatalf("rect %+v: pixels differ from full top-down copy", tc)
		}
	}
}

func TestFramebufferWriteRectTopDownRejectsRectOutsideOrShortDst(t *testing.T) {
	c := newRectReadClient(4, 4)
	cases := []struct {
		name       string
		dst        int
		x, y, w, h int
	}{
		{"past right edge", 16 * 4, 2, 0, 3, 1},
		{"past bottom edge", 16 * 4, 0, 3, 1, 2},
		{"negative origin", 16 * 4, -1, 0, 2, 2},
		{"empty", 16 * 4, 0, 0, 0, 2},
		{"dst too small", 3, 0, 0, 1, 1},
	}
	for _, tc := range cases {
		fw, fh := c.FramebufferWriteRectTopDown(make([]byte, tc.dst), tc.x, tc.y, tc.w, tc.h)
		if fw != 0 || fh != 0 {
			t.Fatalf("%s: dims = %dx%d, want 0x0", tc.name, fw, fh)
		}
	}
	if fw, fh := (&Client{}).FramebufferWriteRectTopDown(make([]byte, 4), 0, 0, 1, 1); fw != 0 || fh != 0 {
		t.Fatalf("no framebuffer: dims = %dx%d, want 0x0", fw, fh)
	}
}
