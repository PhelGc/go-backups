package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	dir := "backups-server"
	os.MkdirAll(dir, 0750)

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

		file, header, err := r.FormFile("backup_file")
		if err != nil {
			http.Error(w, "campo backup_file no encontrado: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		dst := filepath.Join(dir, header.Filename)
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

		msg := fmt.Sprintf(`{"status":"ok","file":"%s","bytes":%d}`, header.Filename, n)
		log.Printf("recibido: %s (%.2f KB)", header.Filename, float64(n)/1024)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msg))
	})

	log.Println("servidor escuchando en http://localhost:8080")
	log.Println("endpoint: POST http://localhost:8080/upload  (campo: backup_file)")
	log.Println("archivos guardados en:", dir)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
