package httpserver

import (
	"fmt"
	"net/http"
)

func SetBinaryFileHeader(header http.Header, filename string) {
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
}
