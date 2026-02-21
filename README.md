# GoBackups

Herramienta CLI en Go para realizar backups de bases de datos MySQL/MariaDB. Diseñada para no bloquear la base de datos, comprimir en streaming sin cargar el archivo completo en RAM, y enviar los backups a un servidor propio con organizacion por cliente y base de datos.

---

## Caracteristicas

- Backup sin bloqueo via `--single-transaction` (MVCC de InnoDB)
- Compresion en streaming: gzip o zstd, sin buffering del archivo completo
- Multiples bases de datos por job: cada una genera su propio archivo
- Almacenamiento local (escritura atomica) o HTTP multipart upload al servidor propio
- Organizacion de carpetas en el servidor: `{cliente}/{base_de_datos}/archivo`
- Modo staged: dump a disco local primero, upload con reintentos sin repetir el dump
- Limpieza automatica de archivos staged viejos
- Reintentos con backoff exponencial
- Notificacion via webhook POST JSON al completar o fallar, con detalle por base de datos
- Programacion tipo cron con modo daemon
- Cancelacion limpia via SIGINT / SIGTERM
- Contrasena pasada via `MYSQL_PWD` (no visible en listado de procesos)

---

## Requisitos

- Go 1.22 o superior
- `mysqldump` disponible en el PATH

---

## Build

```bat
build.bat
```

Genera los tres binarios sin simbolos de debug (`-s -w`):

| Binario | Descripcion |
|---------|-------------|
| `gobackups.exe` | Cliente: ejecuta los backups |
| `backupserver.exe` | Servidor: recibe y almacena los backups |
| `webhookserver.exe` | Servidor de prueba para notificaciones webhook |

---

## gobackups (cliente)

### Configuracion

La configuracion se define en un archivo YAML. Ver [config/example.yaml](config/example.yaml) para un ejemplo completo.

```yaml
version: "1"

jobs:
  - name: full-backup
    client: nombre-cliente          # opcional: organiza carpetas en el servidor
    schedule: "0 2 * * *"

    database:
      host: 127.0.0.1
      port: 3306
      user: backup_user
      password: "${MYSQL_BACKUP_PASSWORD}"
      databases:                    # multiples bases de datos
        - produccion
        - analytics
        - reportes

    compression:
      kind: zstd    # "gzip" o "zstd"
      level: 3

    storage:
      kind: http
      http:
        url: http://tu-servidor:8080/upload
        field_name: backup_file
        timeout_seconds: 3600
        headers:
          X-Api-Key: "${BACKUP_API_KEY}"
        stage_path: C:\backups\staged       # dump local antes de subir
        stage_max_age_hours: 48             # limpiar staged files con mas de 48h

    notify:
      webhook_url: https://hooks.ejemplo.com/notify
      headers:
        Authorization: "Bearer ${NOTIFY_TOKEN}"

    retry:
      max_attempts: 3
      delay_seconds: 30
```

**Campos de `database`:**

| Campo | Descripcion |
|-------|-------------|
| `database` | Una sola base de datos (compatibilidad) |
| `databases` | Lista de bases de datos; si se especifica, tiene prioridad |
| `flags` | Flags extra para mysqldump (ej: `--no-tablespaces`) |

**Campos de `storage.http`:**

| Campo | Descripcion |
|-------|-------------|
| `url` | Endpoint del servidor |
| `field_name` | Nombre del campo multipart (normalmente `backup_file`) |
| `headers` | Headers HTTP adicionales (API keys, tokens) |
| `stage_path` | Si se configura, el dump se escribe aqui primero y luego se sube. Si el upload falla, solo se reintenta el upload sin repetir el dump |
| `stage_max_age_hours` | Elimina archivos del stage_path con mas de N horas al inicio de cada run. 0 = desactivado |

### Compresion

| Kind | Extension | Descripcion |
|------|-----------|-------------|
| `gzip` | `.sql.gz` | Estandar, ampliamente compatible |
| `zstd` | `.sql.zst` | Mas rapido y mejor ratio; recomendado |

Niveles zstd: 1=rapido, 3=balance (default), 7=mejor compresion.
Niveles gzip: 1=rapido, 6=balance (default), 9=maximo.

### Nombre del archivo de backup

```
{job}_{base_de_datos}_{timestamp}{ext_dump}{ext_compresion}
```

Ejemplo: `full-backup_produccion_20260220T020000Z.sql.zst`

### Comandos

```bat
# Validar la configuracion
gobackups.exe validate -c all-dbs.yaml

# Listar jobs
gobackups.exe list -c all-dbs.yaml

# Ejecutar un job especifico
gobackups.exe run -c all-dbs.yaml --job full-backup

# Ver que haria sin ejecutar
gobackups.exe run -c all-dbs.yaml --dry-run

# Modo daemon (cron)
gobackups.exe daemon -c all-dbs.yaml
```

**Flags globales:**

| Flag | Default | Descripcion |
|------|---------|-------------|
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--log-format` | `text` | `text` o `json` |

### Schedule (cron)

Sintaxis estandar de 5 campos:

```
"0 2 * * *"    todos los dias a las 02:00
"0 4 * * 6"    todos los sabados a las 04:00
"0 */6 * * *"  cada 6 horas
""             sin schedule (solo manual)
```

### Notificaciones webhook

Payload JSON enviado al completar o fallar:

```json
{
  "job_name": "full-backup",
  "status": "success",
  "started_at": "2026-02-20T02:00:00Z",
  "finished_at": "2026-02-20T02:01:11Z",
  "duration_ms": 71000,
  "total_bytes": 267172115,
  "databases": [
    { "database": "produccion", "file": "full-backup_produccion_20260220T020000Z.sql.zst", "bytes": 267040244 },
    { "database": "analytics",  "file": "full-backup_analytics_20260220T020000Z.sql.zst",  "bytes": 131871 },
    { "database": "reportes",   "file": "full-backup_reportes_20260220T020000Z.sql.zst",   "bytes": 0, "error": "mysqldump exited with error: exit status 2" }
  ]
}
```

Si algun job falla, `status` es `"failure"` y cada base de datos con error incluye el campo `error`.

---

## backupserver (servidor)

Recibe backups via HTTP multipart, los organiza por cliente y base de datos, y aplica retencion automatica.

### Uso

```bat
backupserver.exe --api-key "clave-secreta" --keep-days 30 --dir D:\backups --port 8080
```

**Flags:**

| Flag | Default | Descripcion |
|------|---------|-------------|
| `--port` | `8080` | Puerto de escucha |
| `--dir` | `backups` | Directorio base |
| `--api-key` | ` ` | Clave requerida en el header `X-Api-Key`. Vacio = sin autenticacion |
| `--keep-days` | `30` | Dias de retencion por carpeta. 0 = sin limite |

### Estructura de carpetas generada

```
backups/
  cliente-a/
    produccion/
      full-backup_produccion_20260220T020000Z.sql.zst
      full-backup_produccion_20260221T020000Z.sql.zst
    analytics/
      full-backup_analytics_20260220T020000Z.sql.zst
  cliente-b/
    main_db/
      full-backup_main_db_20260220T020000Z.sql.zst
```

### Endpoints

| Endpoint | Descripcion |
|----------|-------------|
| `POST /upload` | Recibe un backup. Campos: `backup_file` (archivo), `client`, `database`, `job_name` |
| `GET /health` | Devuelve `{"status":"ok"}` |

---

## webhookserver (pruebas locales)

Servidor de prueba para verificar notificaciones webhook. Imprime los resultados en consola de forma legible.

```bat
webhookserver.exe
# Escucha en http://localhost:8081/webhook
```

---

## Preparacion de usuario MySQL

```sql
CREATE USER 'backup_user'@'%' IDENTIFIED BY 'contrasena_segura';
GRANT SELECT, SHOW VIEW, TRIGGER, LOCK TABLES, PROCESS, EVENT
    ON *.* TO 'backup_user'@'%';
FLUSH PRIVILEGES;
```

Si el usuario no tiene `PROCESS`:

```yaml
database:
  flags:
    - "--no-tablespaces"
```

---

## Arquitectura interna

```
mysqldump stdout
      |
      v  goroutine: io.Copy(compressor, dumpReader)
  compressor (gzip/zstd, streaming)
      |
      v  io.Pipe (back-pressure automatico)
  pipeReader
      |
      v  storage.Store(ctx, filename, pipeReader)
  [local]  archivo.tmp -> os.Rename()
  [http]   multipart (client+database+job_name) -> HTTP request body
```

La contrapresion es automatica: si el destino es lento, el compresor frena, que frena a mysqldump. El uso de memoria es constante independientemente del tamano de la base de datos.

---

## Dependencias

| Libreria | Version | Uso |
|----------|---------|-----|
| `github.com/spf13/cobra` | v1.8.1 | CLI framework |
| `github.com/klauspost/compress` | v1.17.11 | Compresion zstd en streaming |
| `github.com/robfig/cron/v3` | v3.0.1 | Parsing de cron y daemon scheduling |
| `gopkg.in/yaml.v3` | v3.0.1 | Parsing de YAML |

---

## Licencia

MIT
