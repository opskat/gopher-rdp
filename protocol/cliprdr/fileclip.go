package cliprdr

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	FileGroupDescriptorW = "FileGroupDescriptorW"
	defaultFileGroupID   = uint32(0xc001)

	FileContentsSize  = uint32(0x00000001)
	FileContentsRange = uint32(0x00000002)

	fileDescriptorSize     = 592
	fdAttributes           = uint32(0x00000004)
	fdWriteTime            = uint32(0x00000020)
	fdFileSize             = uint32(0x00000040)
	fdProgressUI           = uint32(0x00004000)
	fileAttributeNormal    = uint32(0x00000080)
	fileAttributeDirectory = uint32(0x00000010)
)

// LocalFile describes a clipboard file without requiring it to be loaded into
// memory. ReadRange is called when the server requests a byte range.
type LocalFile struct {
	Name      string
	Size      int64
	ModTime   time.Time
	ReadRange func(offset int64, length int) ([]byte, error)
}

// FileDescriptor is the portable subset of FILEDESCRIPTORW used by callers.
type FileDescriptor struct {
	Name        string
	Size        int64
	ModTime     time.Time
	IsDirectory bool
}

type FileContentsRequest struct {
	StreamID  uint32
	ListIndex uint32
	Flags     uint32
	Offset    int64
	Requested uint32
}

func encodeFileContentsRequest(req FileContentsRequest) []byte {
	payload := make([]byte, 24)
	binary.LittleEndian.PutUint32(payload[0:4], req.StreamID)
	binary.LittleEndian.PutUint32(payload[4:8], req.ListIndex)
	binary.LittleEndian.PutUint32(payload[8:12], req.Flags)
	binary.LittleEndian.PutUint32(payload[12:16], uint32(req.Offset))
	binary.LittleEndian.PutUint32(payload[16:20], uint32(uint64(req.Offset)>>32))
	binary.LittleEndian.PutUint32(payload[20:24], req.Requested)
	return encodePDU(CBFileContentsRequest, 0, payload)
}

func decodeFileContentsRequest(payload []byte) (FileContentsRequest, error) {
	if len(payload) < 24 {
		return FileContentsRequest{}, fmt.Errorf("cliprdr: file contents request too short (%d bytes)", len(payload))
	}
	offset := uint64(binary.LittleEndian.Uint32(payload[12:16])) |
		uint64(binary.LittleEndian.Uint32(payload[16:20]))<<32
	return FileContentsRequest{
		StreamID:  binary.LittleEndian.Uint32(payload[0:4]),
		ListIndex: binary.LittleEndian.Uint32(payload[4:8]),
		Flags:     binary.LittleEndian.Uint32(payload[8:12]),
		Offset:    int64(offset),
		Requested: binary.LittleEndian.Uint32(payload[20:24]),
	}, nil
}

func encodeFileContentsResponse(streamID uint32, ok bool, data []byte) []byte {
	payload := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(payload[:4], streamID)
	copy(payload[4:], data)
	flags := CBResponseOK
	if !ok {
		flags = CBResponseFail
	}
	return encodePDU(CBFileContentsResponse, flags, payload)
}

func encodeFileGroupDescriptorW(files []LocalFile) ([]byte, error) {
	payload := make([]byte, 4+len(files)*fileDescriptorSize)
	binary.LittleEndian.PutUint32(payload[:4], uint32(len(files)))
	for i, file := range files {
		if file.Size < 0 || file.ReadRange == nil {
			return nil, fmt.Errorf("cliprdr: invalid local file %q", file.Name)
		}
		name, err := clipboardFileName(file.Name)
		if err != nil {
			return nil, err
		}
		d := payload[4+i*fileDescriptorSize : 4+(i+1)*fileDescriptorSize]
		binary.LittleEndian.PutUint32(d[0:4], fdAttributes|fdWriteTime|fdFileSize|fdProgressUI)
		binary.LittleEndian.PutUint32(d[36:40], fileAttributeNormal)
		binary.LittleEndian.PutUint64(d[56:64], windowsFileTime(file.ModTime))
		binary.LittleEndian.PutUint32(d[64:68], uint32(uint64(file.Size)>>32))
		binary.LittleEndian.PutUint32(d[68:72], uint32(file.Size))
		writeUTF16Fixed(d[72:592], name)
	}
	return payload, nil
}

func decodeFileGroupDescriptorW(payload []byte) ([]FileDescriptor, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("cliprdr: file group descriptor too short")
	}
	count := int(binary.LittleEndian.Uint32(payload[:4]))
	if count < 0 || len(payload) < 4+count*fileDescriptorSize {
		return nil, fmt.Errorf("cliprdr: truncated file group descriptor")
	}
	files := make([]FileDescriptor, count)
	for i := range count {
		d := payload[4+i*fileDescriptorSize : 4+(i+1)*fileDescriptorSize]
		size := uint64(binary.LittleEndian.Uint32(d[68:72])) |
			uint64(binary.LittleEndian.Uint32(d[64:68]))<<32
		if size > math.MaxInt64 {
			return nil, fmt.Errorf("cliprdr: clipboard file size overflows int64")
		}
		files[i] = FileDescriptor{
			Name:        readUTF16Fixed(d[72:592]),
			Size:        int64(size),
			ModTime:     timeFromWindowsFileTime(binary.LittleEndian.Uint64(d[56:64])),
			IsDirectory: binary.LittleEndian.Uint32(d[36:40])&fileAttributeDirectory != 0,
		}
	}
	return files, nil
}

func clipboardFileName(name string) (string, error) {
	name = strings.ReplaceAll(name, "/", `\`)
	name = strings.TrimPrefix(name, `.\`)
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, `..\`) || name == ".." {
		return "", fmt.Errorf("cliprdr: unsafe clipboard file name %q", name)
	}
	if len(utf16.Encode([]rune(name))) >= 260 {
		return "", fmt.Errorf("cliprdr: clipboard file name too long %q", name)
	}
	return name, nil
}

func writeUTF16Fixed(dst []byte, value string) {
	units := utf16.Encode([]rune(value))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(dst[i*2:], unit)
	}
}

func readUTF16Fixed(src []byte) string {
	units := make([]uint16, 0, len(src)/2)
	for i := 0; i+1 < len(src); i += 2 {
		unit := binary.LittleEndian.Uint16(src[i:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

func windowsFileTime(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.Unix()+11_644_473_600)*10_000_000 + uint64(t.Nanosecond()/100)
}

func timeFromWindowsFileTime(value uint64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	seconds := int64(value/10_000_000) - 11_644_473_600
	nanos := int64(value%10_000_000) * 100
	return time.Unix(seconds, nanos)
}
