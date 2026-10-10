// release creates a portable ZIP and optionally an Ed25519-signed manifest.
package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"linklan/internal/model"
	"linklan/internal/updates"
)

func fail(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func main() {
	bundle := flag.String("bundle", "", "Directory with LinkLAN.exe and wintun.dll")
	output := flag.String("output", "releases", "Output directory")
	version := flag.String("version", model.Version, "Release version")
	keyFile := flag.String("key-file", "", "Private Ed25519 key, raw base64; missing file means unsigned development release")
	generate := flag.String("generate-key", "", "Create a new signing key; print only public key")
	printPublic := flag.Bool("print-public", false, "Print public key from key-file")
	flag.Parse()
	if *generate != "" {
		pub, key, e := ed25519.GenerateKey(rand.Reader)
		fail(e)
		f, e := os.OpenFile(*generate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		fail(e)
		_, e = f.WriteString(base64.RawStdEncoding.EncodeToString(key))
		fail(e)
		fail(f.Close())
		fmt.Println(base64.RawStdEncoding.EncodeToString(pub))
		return
	}
	var key ed25519.PrivateKey
	if *keyFile != "" {
		raw, e := os.ReadFile(*keyFile)
		if e == nil {
			b, e := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(raw)))
			fail(e)
			if len(b) != 64 {
				fail(fmt.Errorf("invalid Ed25519 private key"))
			}
			key = ed25519.PrivateKey(b)
		} else if !os.IsNotExist(e) || *printPublic {
			fail(e)
		}
	}
	if *printPublic {
		if key == nil {
			fail(fmt.Errorf("key required"))
		}
		fmt.Println(base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
		return
	}
	if !updates.Newer(*version, "0.0.0") || *bundle == "" {
		fail(fmt.Errorf("valid -version and -bundle are required"))
	}
	fail(os.MkdirAll(*output, 0755))
	name := "LinkLAN-Windows-x64-v" + *version + ".zip"
	path := filepath.Join(*output, name)
	out, e := os.Create(path)
	fail(e)
	writer := zip.NewWriter(out)
	for _, name := range []string{"LinkLAN.exe", "wintun.dll", "WINTUN-LICENSE.txt", "README.txt", "Run-LinkLAN.cmd", "THIRD_PARTY_NOTICES.txt"} {
		in, e := os.Open(filepath.Join(*bundle, name))
		fail(e)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		w, e := writer.CreateHeader(header)
		fail(e)
		_, e = io.Copy(w, in)
		in.Close()
		fail(e)
	}
	fail(writer.Close())
	fail(out.Close())
	b, e := os.ReadFile(path)
	fail(e)
	hash := sha256.Sum256(b)
	m := updates.Manifest{Version: *version, URL: "/downloads/" + name, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(b))}
	if key != nil {
		m.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, m.Message()))
		fail(m.Verify(base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))))
	}
	data, e := json.MarshalIndent(m, "", "  ")
	fail(e)
	fail(os.WriteFile(filepath.Join(*output, "latest.json"), append(data, '\n'), 0644))
	fail(os.WriteFile(filepath.Join(*output, "SHA256SUMS.txt"), []byte(m.SHA256+"  "+name+"\n"), 0644))
	fmt.Printf("Created %s; signed=%v\n", name, key != nil)
}
