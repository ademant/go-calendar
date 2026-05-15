package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/avif"
	"github.com/gorilla/mux"
	xdraw "golang.org/x/image/draw"
)

// GET /api/v1/images/{event_id}
func getEventImage(w http.ResponseWriter, r *http.Request) {
	eventID := mux.Vars(r)["event_id"]

	// Validate event_id is a plain integer to prevent path traversal
	for _, c := range eventID {
		if c < '0' || c > '9' {
			http.Error(w, "Invalid event ID", http.StatusBadRequest)
			return
		}
	}

	imgPath := filepath.Join(config.Server.ImagesDir, eventID+".avif")
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/avif")
	http.ServeFile(w, r, imgPath)
}

// fitImage scales img down to fit within maxW x maxH, preserving aspect ratio.
// Returns the original if it already fits.
func fitImage(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	// Scale factor that fits both dimensions
	scaleW := float64(maxW) / float64(w)
	scaleH := float64(maxH) / float64(h)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// DELETE /api/v1/images/{event_id}
func deleteEventImage(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	eventID := mux.Vars(r)["event_id"]
	for _, c := range eventID {
		if c < '0' || c > '9' {
			http.Error(w, "Invalid event ID", http.StatusBadRequest)
			return
		}
	}

	imgPath := filepath.Join(config.Server.ImagesDir, eventID+".avif")
	if err := os.Remove(imgPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Image not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/images/{event_id}
func uploadEventImage(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser && userRole != RolePublisher {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	eventID := mux.Vars(r)["event_id"]

	var id int
	err := db.QueryRow("SELECT id FROM events WHERE id = ?", eventID).Scan(&id)
	if err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := r.ParseMultipartForm(config.Server.MaxBodyBytes); err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Missing or unreadable 'image' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read the first 512 bytes to detect the content type from magic bytes
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(http.DetectContentType(head[:n]), "image/") {
		http.Error(w, "File is not an image", http.StatusUnsupportedMediaType)
		return
	}

	// Reconstruct the full reader (prepend the already-read bytes)
	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head[:n]), file))
	if err != nil {
		http.Error(w, "Unsupported or invalid image format", http.StatusBadRequest)
		return
	}

	img = fitImage(img, config.Server.ImageXMax, config.Server.ImageYMax)

	if err := os.MkdirAll(config.Server.ImagesDir, 0o755); err != nil {
		http.Error(w, "Failed to create images directory", http.StatusInternalServerError)
		return
	}

	outPath := filepath.Join(config.Server.ImagesDir, eventID+".avif")
	f, err := os.Create(outPath)
	if err != nil {
		http.Error(w, "Failed to create output file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	if err := avif.Encode(f, img); err != nil {
		http.Error(w, "Failed to encode AVIF", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"path": outPath})
}
