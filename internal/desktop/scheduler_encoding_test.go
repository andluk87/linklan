package desktop

import (
	"encoding/binary"
	"encoding/xml"
	"golang.org/x/text/encoding/charmap"
	"testing"
	"unicode/utf16"
)

func TestSchedulerXMLDecodesRussianTaskInEveryOutputEncoding(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-16"?><Task><Description>LinkLAN: запуск в трее</Description><Command>C:\Пользователи\Андрей\LinkLAN.exe</Command></Task>`
	cases := map[string][]byte{"utf8": []byte(doc), "utf8-bom": append([]byte{239, 187, 191}, []byte(doc)...)}
	oem, e := charmap.CodePage866.NewEncoder().Bytes([]byte(doc))
	if e != nil {
		t.Fatal(e)
	}
	cases["russian-oem-866"] = oem
	for _, be := range []bool{false, true} {
		var order binary.ByteOrder = binary.LittleEndian
		name := "utf16le"
		bom := []byte{255, 254}
		if be {
			order = binary.BigEndian
			name = "utf16be"
			bom = []byte{254, 255}
		}
		words := utf16.Encode([]rune(doc))
		raw := make([]byte, len(words)*2)
		for i, v := range words {
			order.PutUint16(raw[i*2:], v)
		}
		cases[name] = append(bom, raw...)
		cases[name+"-no-bom"] = raw
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			normalized, e := normalizeSchedulerOutput(raw, charmap.CodePage866.NewDecoder().Bytes)
			if e != nil {
				t.Fatal(e)
			}
			var task struct{ Description, Command string }
			if e = xml.Unmarshal(normalized, &task); e != nil {
				t.Fatal(e)
			}
			if task.Description != "LinkLAN: запуск в трее" || task.Command != `C:\Пользователи\Андрей\LinkLAN.exe` {
				t.Fatalf("task text corrupted: %+v", task)
			}
		})
	}
}
