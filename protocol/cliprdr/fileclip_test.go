package cliprdr

import (
	"encoding/binary"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSetLocalFilesAdvertisesFileGroupDescriptor(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.useLongFormatNames = true

	err := h.SetLocalFiles([]LocalFile{{
		Name: "report.txt", Size: 5, ModTime: time.Unix(1_700_000_000, 0),
		ReadRange: func(offset int64, length int) ([]byte, error) {
			return []byte("hello")[offset : offset+int64(length)], nil
		},
	}})
	if err != nil {
		t.Fatalf("SetLocalFiles: %v", err)
	}

	_, _, payload, err := decodePDU(sent[len(sent)-1])
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	formats := decodeFormatListLong(payload)
	if !containsFormatName(formats, FileGroupDescriptorW) {
		t.Fatalf("formats = %+v, want %q", formats, FileGroupDescriptorW)
	}
}

func TestLocalFileDescriptorAndContentsAreStreamed(t *testing.T) {
	content := []byte("hello world")
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.localFiles = []LocalFile{{
		Name: "report.txt", Size: int64(len(content)), ModTime: time.Unix(1_700_000_000, 0),
		ReadRange: func(offset int64, length int) ([]byte, error) {
			return append([]byte(nil), content[offset:offset+int64(length)]...), nil
		},
	}}
	h.fileGroupFormatID = 0xc001

	h.ProcessPDU(encodeFormatDataRequest(h.fileGroupFormatID))
	_, flags, descriptors, _ := decodePDU(sent[len(sent)-1])
	if flags != CBResponseOK {
		t.Fatalf("descriptor response flags = %#x", flags)
	}
	files, err := decodeFileGroupDescriptorW(descriptors)
	if err != nil {
		t.Fatalf("decodeFileGroupDescriptorW: %v", err)
	}
	if len(files) != 1 || files[0].Name != "report.txt" || files[0].Size != int64(len(content)) {
		t.Fatalf("files = %+v", files)
	}

	sent = nil
	h.ProcessPDU(encodeFileContentsRequest(FileContentsRequest{StreamID: 7, ListIndex: 0, Flags: FileContentsRange, Offset: 6, Requested: 5}))
	msgType, flags, payload, _ := decodePDU(sent[len(sent)-1])
	if msgType != CBFileContentsResponse || flags != CBResponseOK {
		t.Fatalf("response type=%#x flags=%#x", msgType, flags)
	}
	if binary.LittleEndian.Uint32(payload[:4]) != 7 || string(payload[4:]) != "world" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestLocalFileDescriptorRequestsNativeShellProgressUI(t *testing.T) {
	descriptors, err := encodeFileGroupDescriptorW([]LocalFile{{
		Name: "large.iso", Size: 8 << 30, ModTime: time.Unix(1_700_000_000, 0),
		ReadRange: func(int64, int) ([]byte, error) { return nil, nil },
	}})
	if err != nil {
		t.Fatalf("encodeFileGroupDescriptorW: %v", err)
	}

	flags := binary.LittleEndian.Uint32(descriptors[4:8])
	if flags&fdProgressUI == 0 {
		t.Fatalf("descriptor flags = %#08x, want FD_PROGRESSUI", flags)
	}
}

func TestRemoteFileDescriptorsAndRangesAreDelivered(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.useLongFormatNames = true

	var available bool
	h.OnRemoteFilesAvailable(func() { available = true })
	h.ProcessPDU(encodeFormatListLong([]FormatListEntry{{FormatID: 0xc123, FormatName: FileGroupDescriptorW}}))
	if !available {
		t.Fatal("remote file callback was not invoked")
	}

	var descriptors []FileDescriptor
	h.OnFileDescriptors(func(files []FileDescriptor) { descriptors = files })
	if err := h.RequestRemoteFiles(); err != nil {
		t.Fatalf("RequestRemoteFiles: %v", err)
	}
	descriptorWire, err := encodeFileGroupDescriptorW([]LocalFile{{Name: "remote.txt", Size: 5, ReadRange: func(int64, int) ([]byte, error) { return nil, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	h.ProcessPDU(encodeFormatDataResponse(true, descriptorWire))
	if len(descriptors) != 1 || descriptors[0].Name != "remote.txt" || descriptors[0].Size != 5 {
		t.Fatalf("descriptors = %+v", descriptors)
	}

	var streamID uint32
	var rangeData []byte
	h.OnFileContents(func(id uint32, data []byte, err error) {
		streamID, rangeData = id, data
	})
	if err := h.RequestRemoteFileRange(0, 9, 1, 3); err != nil {
		t.Fatalf("RequestRemoteFileRange: %v", err)
	}
	h.ProcessPDU(encodeFileContentsResponse(9, true, []byte("ell")))
	if streamID != 9 || string(rangeData) != "ell" {
		t.Fatalf("streamID=%d data=%q", streamID, rangeData)
	}
}

func containsFormatName(formats []FormatListEntry, name string) bool {
	for _, format := range formats {
		if strings.EqualFold(format.FormatName, name) {
			return true
		}
	}
	return false
}
