package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	baseDir := "backups-server"
	os.MkdirAll(baseDir, 0750)

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "solo POST", http.StatusMethodNotAllowed)
			return
		}

		// 10 GB max
		if err := r.ParseMultipartForm(10 << 30); err != nil {
			http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Leer campos de metadata (opcionales)
		client := strings.TrimSpace(r.FormValue("client"))
		database := strings.TrimSpace(r.FormValue("database"))
		jobName := strings.TrimSpace(r.FormValue("job_name"))

		file, header, err := r.FormFile("backup_file")
		if err != nil {
			http.Error(w, "campo backup_file no encontrado: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Construir ruta de destino:
		// Con metadata:    backups-server/{client}/{database}/filename
		// Sin metadata:    backups-server/filename  (comportamiento anterior)
		var destDir string
		if client != "" && database != "" {
			destDir = filepath.Join(baseDir, sanitize(client), sanitize(database))
		} else if client != "" {
			destDir = filepath.Join(baseDir, sanitize(client))
		} else {
			destDir = baseDir
		}

		if err := os.MkdirAll(destDir, 0750); err != nil {
			http.Error(w, "no se pudo crear directorio: "+err.Error(), http.StatusInternalServerError)
			return
		}

		dst := filepath.Join(destDir, header.Filename)
		out, err := os.Create(dst)
		if err != nil {
			http.Error(w, "no se pudo crear archivo: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer out.Close()

		n, err := io.Copy(out, file)
		if err != nil {
			http.Error(w, "error al escribir: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("recibido: client=%q db=%q job=%q file=%s (%.2f KB)",
			client, database, jobName, header.Filename, float64(n)/1024)

		msg := fmt.Sprintf(
			`{"status":"ok","file":"%s","path":"%s","bytes":%d}`,
			header.Filename, filepath.ToSlash(dst), n,
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msg))
	})

	log.Println("servidor escuchando en http://localhost:8080")
	log.Println("endpoint: POST http://localhost:8080/upload  (campo: backup_file)")
	log.Printf("estructura de carpetas: %s/{client}/{database}/archivo\n", baseDir)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// sanitize elimina caracteres peligrosos de un nombre de directorio.
// Solo permite letras, números, guiones y guiones bajos.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
