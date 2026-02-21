# GoBackups

Herramienta CLI en Go para realizar backups de bases de datos MySQL/MariaDB de hasta 100 GB. Diseñada para no bloquear la base de datos, comprimir en streaming sin cargar el archivo en RAM, y ser extensible para múltiples destinos de almacenamiento.

---

## Características

- Backup sin bloqueo via `--single-transaction` (MVCC de InnoDB, compatible con tablas particionadas)
- Compresion en streaming: gzip o zstd, sin buffering del archivo completo
- Almacenamiento local (escritura atomica) o HTTP multipart upload al servidor propio
- Reintentos con backoff exponencial
- Notificacion via webhook POST JSON al completar o fallar
- Programacion tipo cron con modo daemon
- Cancelacion limpia via SIGINT / SIGTERM
- Contraseña pasada via `MYSQL_PWD` (no visible en listado de procesos)

---

## Requisitos

- Go 1.22 o superior
- `mysqldump` disponible en el PATH (incluido en MySQL Community Server o MySQL Workbench)

---

## Instalacion

```bash
git clone https://github.com/tu-usuario/gobackups
cd gobackups
go build -o gobackups ./cmd/gobackups
```

En Windows:

```bash
go build -o gobackups.exe ./cmd/gobackups
```

---

## Configuracion

La configuracion se define en un archivo YAML. Ver [config/example.yaml](config/example.yaml) para un ejemplo completo.

### Estructura basica

```yaml
version: "1"

jobs:
  - name: mi-backup
    schedule: "0 2 * * *"   # cron de 5 campos: min hora dia mes diaSemana

    database:
      host: 127.0.0.1
      port: 3306
      user: backup_user
      password: "${MYSQL_BACKUP_PASSWORD}"   # variable de entorno
      database: nombre_base_de_datos

    compression:
      kind: zstd    # "gzip" o "zstd"
      level: 3

    storage:
      kind: local   # "local" o "http"
      local:
        path: /var/backups/mysql

    notify:
      webhook_url: https://hooks.example.com/notify
      headers:
        Authorization: "Bearer ${NOTIFY_TOKEN}"

    retry:
      max_attempts: 3
      delay_seconds: 30
```

### Variables de entorno

Las referencias `${VAR}` en el YAML se expanden con las variables de entorno del proceso antes de parsear el archivo. Util para contraseñas y tokens.

### Compresion

| Kind | Extension | Descripcion |
|------|-----------|-------------|
| `gzip` | `.sql.gz` | Estandar, ampliamente compatible |
| `zstd` | `.sql.zst` | Mas rapido y mejor ratio; recomendado para bases grandes |

Niveles de zstd: 1 = mas rapido, 3 = balance (default), 7 = mejor compresion, 9+ = maximo.

Niveles de gzip: 1 = mas rapido, 6 = balance (default), 9 = maximo.

### Almacenamiento local

```yaml
storage:
  kind: local
  local:
    path: /var/backups/mysql
```

La escritura es atomica: el archivo se escribe primero a un `.tmp` y luego se renombra. Un crash no deja archivos corruptos.

### Almacenamiento HTTP (servidor propio)

```yaml
storage:
  kind: http
  http:
    url: https://tu-servidor.com/api/backups/upload
    field_name: backup_file          # nombre del campo multipart
    timeout_seconds: 3600            # 1 hora; bases grandes necesitan timeouts generosos
    headers:
      X-Api-Key: "${STORAGE_API_KEY}"
```

El upload es streaming: los bytes fluyen de mysqldump al servidor sin cargarse en RAM.

### Notificaciones

El webhook recibe un POST con Content-Type `application/json`:

```json
{
  "job_name": "mi-backup",
  "status": "success",
  "started_at": "2026-02-20T02:00:00Z",
  "finished_at": "2026-02-20T02:04:31Z",
  "duration_ms": 271000,
  "bytes_written": 1048576000,
  "destination": "mi-backup_20260220T020000Z.sql.zst",
  "error": ""
}
```

En caso de fallo, `status` es `"failure"` y `error` contiene el mensaje.

### Reintentos

```yaml
retry:
  max_attempts: 3      # intentos totales (1 = sin reintento)
  delay_seconds: 30    # base del backoff: 30s, 60s, 120s, ...
```

El delay entre intentos crece exponencialmente: `base * 2^(intento-1)`, con un maximo de 10 minutos.

### Schedule (cron)

Se usa sintaxis estandar de 5 campos:

```
┌───── minuto (0-59)
│ ┌───── hora (0-23)
│ │ ┌───── dia del mes (1-31)
│ │ │ ┌───── mes (1-12)
│ │ │ │ ┌───── dia de semana (0-6, domingo=0)
│ │ │ │ │
* * * * *
```

Ejemplos:
- `"0 2 * * *"` — todos los dias a las 02:00
- `"0 4 * * 6"` — todos los sabados a las 04:00
- `"0 */6 * * *"` — cada 6 horas
- `""` — sin schedule (solo manual via `run`)

---

## Uso

### Validar la configuracion

```bash
gobackups validate -c config/mi-config.yaml
```

Muestra un resumen de cada job si la configuracion es valida. Falla con mensaje de error si hay algun campo faltante o invalido.

### Listar jobs

```bash
gobackups list -c config/mi-config.yaml
```

Imprime una tabla con nombre, base de datos, host, compresion, storage y schedule de cada job.

### Ejecutar un backup ahora

```bash
# Ejecutar todos los jobs secuencialmente
gobackups run -c config/mi-config.yaml

# Ejecutar solo un job especifico
gobackups run -c config/mi-config.yaml --job prod-db-local

# Preview: ver que haria sin ejecutar
gobackups run -c config/mi-config.yaml --dry-run
```

### Modo daemon (cron)

```bash
gobackups daemon -c config/mi-config.yaml
```

Ejecuta los jobs segun su `schedule`. Responde a SIGINT (Ctrl+C) y SIGTERM esperando que terminen los jobs en curso antes de salir.

### Flags globales

| Flag | Default | Descripcion |
|------|---------|-------------|
| `--log-level` | `info` | Nivel de log: `debug`, `info`, `warn`, `error` |
| `--log-format` | `text` | Formato de log: `text`, `json` |

---

## Nombre del archivo de backup

El nombre generado sigue el patron:

```
{job}_{timestamp}{ext_dump}{ext_compresion}
```

Ejemplo: `prod-db-local_20260220T020000Z.sql.zst`

---

## Preparacion de usuario MySQL para backups

El usuario de backup necesita permisos minimos:

```sql
CREATE USER 'backup_user'@'%' IDENTIFIED BY 'contraseña_segura';
GRANT SELECT, SHOW VIEW, TRIGGER, LOCK TABLES, PROCESS, EVENT
    ON *.* TO 'backup_user'@'%';
FLUSH PRIVILEGES;
```

Si el usuario no tiene `PROCESS`, agregar `--no-tablespaces` en `database.flags`:

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
  compressor (gzip / zstd, streaming)
      |
      v  io.Pipe (back-pressure automatico)
  pipeReader
      |
      v  storage.Store(ctx, filename, pipeReader)
  [local]  archivo.tmp -> os.Rename()
  [http]   multipart.Writer -> HTTP request body
```

La contrapresion es automatica: si el destino es lento, el compresor frena, que frena a mysqldump. El uso de memoria es constante independientemente del tamaño de la base de datos.

---

## Extensibilidad futura

Para agregar un nuevo destino de almacenamiento (AWS S3, Azure Blob, SFTP, etc.):

1. Crear `internal/storage/s3.go` implementando la interfaz `storage.Writer`
2. Registrarlo en `internal/storage/storage.go` en la funcion `New()`
3. Agregar la configuracion correspondiente en `internal/config/types.go`

El pipeline no cambia.

---

## Dependencias

| Libreria | Version | Uso |
|----------|---------|-----|
| `github.com/spf13/cobra` | v1.8.1 | CLI framework |
| `github.com/klauspost/compress` | v1.17.11 | Compresion zstd en streaming |
| `github.com/robfig/cron/v3` | v3.0.1 | Parsing de cron y daemon scheduling |
| `gopkg.in/yaml.v3` | v3.0.1 | Parsing de YAML |

Compresion gzip usa la libreria estandar de Go (`compress/gzip`).

---

## Licencia

MIT
