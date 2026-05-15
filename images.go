package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	ics "github.com/arran4/golang-ical"
	"github.com/gen2brain/avif"
	"github.com/gorilla/mux"
	xdraw "golang.org/x/image/draw"
)

var errNotImage = errors.New("data is not an image")

// saveImageFromReader decodes image data from r, resizes, and stores as AVIF for the given event ID.
func saveImageFromReader(eventID int, r io.Reader) error {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("read: %w", err)
	}
	if !strings.HasPrefix(http.DetectContentType(head[:n]), "image/") {
		return errNotImage
	}
	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head[:n]), r))
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	img = fitImage(img, config.Server.ImageXMax, config.Server.ImageYMax)
	if err := os.MkdirAll(config.Server.ImagesDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	outPath := filepath.Join(config.Server.ImagesDir, fmt.Sprintf("%d.avif", eventID))
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()
	if err := avif.Encode(f, img); err != nil {
		return fmt.Errorf("encode avif: %w", err)
	}
	return nil
}

// attachImagesFromICalEvent downloads and stores the first image ATTACH from a vevent.
// Skips silently if an image already exists for the event.
func attachImagesFromICalEvent(eventID int, vevent *ics.VEvent) {
	if _, err := os.Stat(filepath.Join(config.Server.ImagesDir, fmt.Sprintf("%d.avif", eventID))); err == nil {
		return
	}
	for _, prop := range vevent.GetProperties(ics.ComponentPropertyAttach) {
		fmttype := prop.ICalParameters["FMTTYPE"]
		if len(fmttype) == 0 || !strings.HasPrefix(fmttype[0], "image/") {
			continue
		}
		if tryAttachImage(eventID, prop) {
			return
		}
	}
}

func tryAttachImage(eventID int, prop *ics.IANAProperty) bool {
	valueType := ""
	if vt := prop.ICalParameters["VALUE"]; len(vt) > 0 {
		valueType = strings.ToUpper(vt[0])
	}

	if valueType == "BINARY" {
		enc := ""
		if e := prop.ICalParameters["ENCODING"]; len(e) > 0 {
			enc = strings.ToUpper(e[0])
		}
		if enc != "BASE64" {
			return false
		}
		data, err := base64.StdEncoding.DecodeString(prop.Value)
		if err != nil {
			log.Printf("iCal ATTACH base64 decode for event %d: %v", eventID, err)
			return false
		}
		if err := saveImageFromReader(eventID, bytes.NewReader(data)); err != nil {
			log.Printf("iCal ATTACH save for event %d: %v", eventID, err)
			return false
		}
		return true
	}

	// URI attachment
	resp, err := fetchClient.Get(prop.Value)
	if err != nil {
		log.Printf("iCal ATTACH fetch for event %d: %v", eventID, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("iCal ATTACH HTTP %d for event %d", resp.StatusCode, eventID)
		return false
	}
	if err := saveImageFromReader(eventID, resp.Body); err != nil {
		log.Printf("iCal ATTACH save for event %d: %v", eventID, err)
		return false
	}
	return true
}

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

	if err := saveImageFromReader(id, file); err != nil {
		if errors.Is(err, errNotImage) {
			http.Error(w, "File is not an image", http.StatusUnsupportedMediaType)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	outPath := filepath.Join(config.Server.ImagesDir, eventID+".avif")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"path": outPath})
}
