package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strconv"
)

// An item in the virtual disk: [Head, Tail)
type Entry struct {
	Head int
	Tail int
}

func (dd *Entry) Overlap(other Entry) bool {
	return (dd.Head >= other.Head && dd.Head < other.Tail) || (dd.Tail > other.Head && dd.Tail <= other.Tail)
}

func NewVirtualDisk() VirtualDisk {
	return VirtualDisk{
		Entries: make([]Entry, 0),
	}
}

type VirtualDisk struct {
	Entries []Entry
	Buffer  bytes.Buffer
}

// Uniform returns a uniformly random float in [0,1).
// https://stackoverflow.com/questions/53277105/generate-uniformly-random-float-which-can-return-all-possible-values
func uniform() float64 {
	sig := rand.Uint64() % (1 << 52)

	return (1 + float64(sig)/(1<<52)) / math.Pow(2, geometric())
}

// Geometric returns a number picked from a geometric
// Distribution of parameter 0.5.
func geometric() float64 {
	b := 1

	for rand.Uint64()%2 == 0 {
		b++
	}

	return float64(b)
}

func (vd *VirtualDisk) Corrupt() []int {
	const maxCorruptionSize = 32

	size := vd.Buffer.Len()

	p := int(uniform() * float64(size))
	n := int(uniform() * float64(min(size-p, maxCorruptionSize)))

	if n == 0 {
		return make([]int, 0)
	}

	raw := vd.Buffer.Bytes()

	// JPEG stream exists between SOI and EOI markers
	var jpegSoiEoiMarkers = []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xD9, // EOI
	}

	// rand.Intn produces a number in [0, size)
	for i := p; i <= p+n; i += 1 {
		// Skip corrupting the beginning and the end of JPEG streams
		if !bytes.Contains(jpegSoiEoiMarkers, []byte{raw[i]}) {
			raw[i] = byte(rand.Intn(256))
		}
	}

	corruptionArea := Entry{
		Head: p,
		Tail: p + n,
	}

	corruptedEntryIndexes := make([]int, 0)

	for index, entry := range vd.Entries {
		if corruptionArea.Overlap(entry) {
			corruptedEntryIndexes = append(corruptedEntryIndexes, index)
		}
	}

	return corruptedEntryIndexes
}

func (vd *VirtualDisk) GetFile(index int) (*bytes.Buffer, *Entry) {
	if index >= len(vd.Entries) || index < 0 {
		return nil, nil
	}

	entry := vd.Entries[index]

	raw := vd.Buffer.Bytes()

	head := entry.Head
	tail := entry.Tail

	data := raw[head:tail]

	return bytes.NewBuffer(data), &entry
}

func (vd *VirtualDisk) Reset() {
	vd.Buffer.Reset()

	vd.Entries = make([]Entry, 0)
}

func (vd *VirtualDisk) AddFile(r io.Reader) error {
	previousTail := vd.Buffer.Len()
	n, err := io.Copy(&vd.Buffer, r)

	if err != nil {
		return err
	}

	vd.Entries = append(vd.Entries, Entry{
		Head: previousTail,
		Tail: int(n) + previousTail,
	})

	return nil
}

var virtualDisk = NewVirtualDisk()

//go:embed client.html
var clientHTML string

func getClient(w http.ResponseWriter, r *http.Request) {
	virtualDisk.Reset()
	load()

	w.WriteHeader(200)
	w.Header().Set("Content-Type", "text/html")

	fmt.Fprint(w, clientHTML)
}

func getPicture(w http.ResponseWriter, r *http.Request) {
	index := 0

	if indexRaw := r.URL.Query().Get("index"); indexRaw != "" {
		if indexParsed, err := strconv.ParseInt(indexRaw, 10, 32); err == nil {
			index = int(indexParsed)
		}
	}

	buffer, _ := virtualDisk.GetFile(index)

	if buffer == nil {
		w.WriteHeader(404)

		return
	}

	w.WriteHeader(200)
	w.Header().Set("Content-Type", "image/jpeg")

	io.Copy(w, buffer)
}

func postCorrupt(w http.ResponseWriter, r *http.Request) {
	corruptedEntryIndexes := virtualDisk.Corrupt()

	w.WriteHeader(200)
	json.NewEncoder(w).Encode(corruptedEntryIndexes)
}

func serve() {
	http.HandleFunc("/", getClient)
	http.HandleFunc("/picture", getPicture)
	http.HandleFunc("/corrupt", postCorrupt)

	if err := http.ListenAndServe(":3333", nil); err != nil {
		log.Printf("error while listen and serve: %v", err)
	}
}

func loadFile(filePath string) {
	f, err := os.OpenFile(filePath, os.O_RDONLY, os.ModePerm)

	if err != nil {
		log.Printf("error while opening file (%s): %s", filePath, err)

		return
	}

	defer f.Close()

	if err := virtualDisk.AddFile(f); err != nil {
		log.Printf("error while reading file: %s", err)
	}
}

func loadDir(dirPath string) {
	entries, err := os.ReadDir(dirPath)

	if err != nil {
		log.Printf("error while reading dir: %s", err)

		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := path.Join(dirPath, entry.Name())

		loadFile(filePath)
	}
}

func load() {
	const defaultDirPath = "./pictures"

	if dirPath := os.Getenv("DIR_PATH"); dirPath != "" {
		loadDir(dirPath)
	} else {
		loadDir(defaultDirPath)
	}
}

func main() {
	serve()
}
