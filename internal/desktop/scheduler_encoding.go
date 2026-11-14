package desktop

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// schtasks can return UTF-16, UTF-8, or OEM-encoded XML whose declaration
// still says UTF-16. Decode actual bytes before passing them to encoding/xml.
func normalizeSchedulerOutput(data []byte, legacy func([]byte) ([]byte, error)) ([]byte, error) {
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		order = binary.LittleEndian
		data = data[2:]
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		order = binary.BigEndian
		data = data[2:]
	case len(data) >= 2 && data[0] != 0 && data[1] == 0:
		order = binary.LittleEndian
	case len(data) >= 2 && data[0] == 0 && data[1] != 0:
		order = binary.BigEndian
	}
	if order != nil {
		if len(data)%2 != 0 {
			return nil, fmt.Errorf("Некорректный UTF-16 в ответе Планировщика заданий")
		}
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = order.Uint16(data[i*2:])
		}
		data = []byte(string(utf16.Decode(words)))
	} else {
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(data) {
			var e error
			data, e = legacy(data)
			if e != nil {
				return nil, fmt.Errorf("Не удалось прочитать кодировку Планировщика заданий: %w", e)
			}
		}
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("Некорректная кодировка ответа Планировщика заданий")
	}
	for _, decl := range []string{`encoding="UTF-16"`, `encoding='UTF-16'`, `encoding="utf-16"`, `encoding='utf-16'`} {
		data = bytes.Replace(data, []byte(decl), []byte(`encoding="UTF-8"`), 1)
	}
	return data, nil
}
