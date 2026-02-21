package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	flagPort     = flag.Int("port", 8080, "Puerto de escucha")
	flagDir      = flag.String("dir", "backups", "Directorio base donde guardar los backups")
	flagAPIKey   = flag.String("api-key", "", "API key requerida en el header X-Api-Key (vacío = sin auth)")
	flagKeepDays = flag.Int("keep-days", 30, "Días de retención por carpeta (0 = sin límite)")
)

func main() {
	flag.Parse()

	if err := os.MkdirAll(*flagDir, 0750); err != nil {
		log.Fatalf("no se pudo crear directorio base %q: %v", *flagDir, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", authMiddleware(handleUpload))
	mux.HandleFunc("/health", handleHealth)

	addr := fmt.Sprintf(":%d", *flagPort)
	log.Println("============================================")
	log.Printf("  GoBackups Server")
	log.Printf("  Escuchando en http://0.0.0.0%s", addr)
	log.Printf("  Directorio: %s", *flagDir)
	if *flagAPIKey != "" {
		log.Printf("  Auth: X-Api-Key requerido")
	} else {
		log.Printf("  Auth: desactivada (recomendado activar con --api-key)")
	}
	if *flagKeepDays > 0 {
		log.Printf("  Retención: %d días por carpeta", *flagKeepDays)
	} else {
		log.Printf("  Retención: sin límite")
	}
	log.Println("============================================")

	log.Fatal(http.ListenAndServe(addr, mux))
}

// authMiddleware valida el header X-Api-Key si se configuró un api-key.
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if *flagAPIKey != "" {
			provided := r.Header.Get("X-Api-Key")
			if provided != *flagAPIKey {
				log.Printf("[AUTH] acceso denegado desde %s", r.RemoteAddr)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "solo POST", http.StatusMethodNotAllowed)
		return
	}

	// 10 GB max en memoria de parseo (el archivo va a disco en streaming)
	if err := r.ParseMultipartForm(10 << 30); err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Leer campos de metadata
	client := sanitize(strings.TrimSpace(r.FormValue("client")))
	database := sanitize(strings.TrimSpace(r.FormValue("database")))
	jobName := strings.TrimSpace(r.FormValue("job_name"))

	file, header, err := r.FormFile("backup_file")
	if err != nil {
		http.Error(w, "campo backup_file no encontrado: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Construir ruta destino según metadata disponible
	var destDir string
	switch {
	case client != "" && database != "":
		destDir = filepath.Join(*flagDir, client, database)
	case client != "":
		destDir = filepath.Join(*flagDir, client)
	default:
		destDir = *flagDir
	}

	if err := os.MkdirAll(destDir, 0750); err != nil {
		http.Error(w, "no se pudo crear directorio: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Escritura atómica: temp file → rename
	finalPath := filepath.Join(destDir, header.Filename)
	tmpPath := fmt.Sprintf("%s.tmp.%d", finalPath, os.Getpid())

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		http.Error(w, "no se pudo crear archivo: "+err.Error(), http.StatusInternalServerError)
		return
	}

	n, err := io.Copy(out, file)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		http.Error(w, "error al escribir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		http.Error(w, "error al finalizar archivo: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[OK] client=%q db=%q job=%q file=%s (%s)",
		client, database, jobName, header.Filename, formatBytes(n))

	// Limpieza de archivos viejos en la carpeta de destino
	if *flagKeepDays > 0 {
		cleanOldFiles(destDir, *flagKeepDays)
	}

	resp := fmt.Sprintf(`{"status":"ok","file":"%s","path":"%s","bytes":%d}`,
		header.Filename, filepath.ToSlash(finalPath), n)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(resp))
}

// cleanOldFiles elimina archivos del directorio con más de keepDays días.
func cleanOldFiles(dir string, keepDays int) {
	cutoff := time.Now().AddDate(0, 0, -keepDays)

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[WARN] retención: no se pudo leer %s: %v", dir, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			age := int(time.Since(info.ModTime()).Hours() / 24)
			if err := os.Remove(path); err != nil {
				log.Printf("[WARN] retención: no se pudo borrar %s: %v", path, err)
			} else {
				log.Printf("[LIMPIEZA] eliminado %s (antigüedad: %d días)", path, age)
			}
		}
	}
}

// sanitize permite solo letras, números, guiones y guiones bajos.
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

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}
