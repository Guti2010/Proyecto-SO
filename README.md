# SO-HTTP/1.0 – Servidor concurrente en Go (proyecto de SO)

Este repositorio contiene una implementación **desde cero** de un servidor HTTP/1.0 (sin TLS) en Go, con **concurrencia por pools**, **colas por comando**, **backpressure**, **métricas internas**, y un conjunto de **endpoints básicos** + **CPU-bound**. Todo corre únicamente con **Docker**/**Docker Compose** (no necesitas Go instalado en tu host).

---

## Cómo ejecutar

```bash
# levantar el server y el contenedor de desarrollo
docker compose up -d --build

# ver logs del servidor
docker compose logs -f http10

# parar todo
docker compose down
```

El servidor queda escuchando en `localhost:8080` y **habla HTTP/1.0**. Para probar con curl:

```bash
curl --http1.0 -i "http://localhost:8080/status"
```

> Nota: Postman envía HTTP/1.1 por defecto; para esta práctica usa curl con `--http1.0` o configura Postman para HTTP/1.0.

---

## Estructura del repo

```
.
├─ cmd/
│  └─ server/
│     └─ main.go              # Arranque del proceso, configuración de pools y listen
├─ internal/
│  ├─ http10/
│  │  ├─ parser.go            # Parser muy simple de HTTP/1.0 (método, ruta, query)
│  │  ├─ query.go             # Decodificación segura de parámetros
│  │  └─ response.go          # Utilidades para escribir respuestas HTTP/1.0
│  ├─ router/
│  │  └─ router.go            # Tabla de rutas -> handlers y registro de pools
│  ├─ server/
│  │  └─ server.go            # Acepta conexiones, despacha por goroutine
│  ├─ handlers/
│  │  ├─ basic.go             # /help, /status, /timestamp, /reverse, /toupper...
│  │  ├─ files.go             # /createfile, /deletefile (con sanitización)
│  │  ├─ cpu.go               # /isprime, /factor, /pi, /mandelbrot, /matrixmul
│  │  └─ (próx: io.go)        # (pendiente) IO-bound: sortfile, wordcount, grep, hashfile, compress
│  ├─ resp/
│  │  └─ resp.go              # Fábricas JSON y texto: 200/400/404/409/429/500/503
│  ├─ sched/
│  │  └─ sched.go             # Pool de workers por comando + cola + métricas/backpressure
│  └─ util/
│     └─ ids.go               # Generador simple de X-Request-Id
├─ data/                      # Carpeta montada en el contenedor (/app/data)
├─ test/
│  └─ ...                     # Pruebas unitarias (un archivo por paquete bajo internal/)
├─ Dockerfile
├─ docker-compose.yml
└─ scripts/
   └─ loadtest.ps1            # Cargas paralelas desde PowerShell (Windows)
```

---

## Diseño en dos capas

1. **Capa de transporte (HTTP/1.0 “raw”)**

   - `internal/server`: acepta `net.Listen`, hace `Accept` en bucle y lanza una goroutine `HandleConn` por socket.  
   - `internal/http10`: parser mínimo de request line y headers relevantes; lectura de cuerpo si aplica; helpers de respuesta (status line + headers + body).

2. **Capa de aplicación**

   - `internal/router`: mapea rutas a funciones de la forma `func(map[string]string) resp.Result`. También **registra** pools/colas por comando en `sched.Manager`.  
   - `internal/handlers`: lógica de cada ruta (CPU-bound, utilitarias, manejo de archivos, etc.).  
   - `internal/sched`: **Pool por comando** con **cola bounded**, **timeout de encolado**, **timeout de ejecución**, **backpressure** (503) y **métricas**.

---

## Concurrencia, colas y métricas

Cada comando puede tener su propio `Pool` con tamaño y capacidad configurables (workers y queue). El **encolado** respeta un timeout; si no hay lugar dentro del plazo, se responde **503 Service Unavailable** con backpressure.

Métricas por comando (ruta `/metrics`):

```json
{
  "sleep": {
    "queue_len": 0,
    "queue_cap": 8,
    "workers": {"total": 2, "busy": 0},
    "submitted": 24,
    "completed": 16,
    "rejected": 8,
    "latency_ms": {"avg_wait": 12.3, "avg_run": 1000.5}
  },
  "spin": { ... }
}
```

Campos:

- `queue_len`, `queue_cap`  
- `workers.total`, `workers.busy`  
- `submitted` (entraron a cola), `completed`, `rejected` (rechazados por backpressure)  
- `latency_ms.avg_wait` (espera en cola), `latency_ms.avg_run` (tiempo de ejecución)

---

## Configuración (variables de entorno)

En `docker-compose.yml` se definen defaults, y `cmd/server/main.go` los lee:

```yaml
services:
  http10:
    environment:
      - WORKERS_SLEEP=2
      - QUEUE_SLEEP=8
      - WORKERS_SPIN=2
      - QUEUE_SPIN=8
      - WORKERS_ISPRIME=2
      - QUEUE_ISPRIME=64
      - WORKERS_FACTOR=2
      - QUEUE_FACTOR=64
      - WORKERS_PI=1
      - QUEUE_PI=8
      - WORKERS_MANDELBROT=1
      - QUEUE_MANDELBROT=4
      - WORKERS_MATRIXMUL=1
      - QUEUE_MATRIXMUL=8
      # (próximos IO-bound)
      - WORKERS_WORDCOUNT=2
      - QUEUE_WORDCOUNT=64
      - WORKERS_GREP=2
      - QUEUE_GREP=64
      - WORKERS_HASHFILE=2
      - QUEUE_HASHFILE=64
```

---

## Endpoints implementados

### Utilitarios / básicos (HTTP/1.0 GET)

- `/help`
- `/status` → JSON con uptime, PID, conexiones atendidas, workers por comando, tamaño de colas…
- `/timestamp`
- `/reverse?text=abcdef`
- `/toupper?text=abcd`

### Archivos (montados en `./data` → `/app/data` dentro del contenedor)

- `/createfile?name=filename&content=txt&repeat=x`  
  Nombre **sanitizado** (sin `../` ni separadores). Crea si no existe; escribe `repeat` veces.
- `/deletefile?name=filename`  
  404 si no existe. Errores de E/S → 500.

### Simulación / carga

- `/sleep?seconds=s` (IO simulado)
- `/simulate?task=sleep|spin&seconds=s` (familia de tareas simples que ejecutan en los pools configurados)

### CPU-bound

- `/isprime?n=NUM` → primalidad por división hasta √n  
- `/factor?n=NUM` → factorización en primos, devuelve pares `[factor, conteo]`  
- `/pi?digits=D&algo=spigot|chudnovsky&timeout_ms=T`
  - **spigot**: decimales en base 10.  
  - **chudnovsky**: serie rápida con `big.Float`.  
  - Respuesta: `{"pi":"3.xxxxx", "truncated":bool, "iterations":k, ...}`.
- `/mandelbrot?width=W&height=H&max_iter=I` → mapa de iteraciones (matriz JSON)  
- `/matrixmul?size=N&seed=S` → producto de matrices NxN pseudoaleatorias; devuelve **SHA-256** del resultado para verificación.

> Endpoints IO-bound **pendientes**: `/sortfile`, `/wordcount`, `/grep`, `/compress`, `/hashfile`.

---

## Ejemplos rápidos

```bash
# métricas
curl --http1.0 -s "http://localhost:8080/metrics" | jq .

# crear y borrar archivos
curl --http1.0 "http://localhost:8080/createfile?name=a.txt&content=Hi&repeat=3"
curl --http1.0 "http://localhost:8080/deletefile?name=a.txt"

# primalidad y factorización
curl --http1.0 "http://localhost:8080/isprime?n=97"
curl --http1.0 "http://localhost:8080/factor?n=360"

# π con 200 dígitos usando Chudnovsky (con timeout)
curl --http1.0 -s "http://localhost:8080/pi?digits=200&algo=chudnovsky&timeout_ms=1500" | jq .

# mandelbrot pequeño
curl --http1.0 -s "http://localhost:8080/mandelbrot?width=64&height=48&max_iter=200" | jq .

# matrixmul y hash
curl --http1.0 -s "http://localhost:8080/matrixmul?size=64&seed=123" | jq .
```

---

## Pruebas y cobertura

Ejecuta las pruebas desde el contenedor de desarrollo:

```bash
# tests
docker compose run --rm dev go test ./... -count=1

# cobertura
docker compose run --rm dev go test ./... -coverprofile=cover.out
docker compose run --rm dev sh -lc 'go tool cover -func=cover.out'
```

Estructura de pruebas:

- Carpeta `test/` con un archivo por paquete (`*_test.go`) que importa `so-http10-demo/internal/...`.  
- Cobertura objetivo ≥ 90% (vamos subiendo a medida que se implementan IO-bound y casos límite).  
- Hay un script de carga para Windows PowerShell:

```powershell
# 50 peticiones en paralelo, luego imprime conteo de códigos
.\scripts\loadtest.ps1 -N 50 -Seconds 10 -WorkersSleep 1 -QueueSleep 1
```

---

## Decisiones de diseño relevantes

- **HTTP/1.0**: se implementa el protocolo de forma explícita y minimalista (sin `net/http`), para exponer el manejo de sockets/buffers y el cierre de conexión por respuesta (`Connection: close`).  
- **Colas por comando**: cada ruta “larga” pasa por `sched.Pool.SubmitAndWait(timeout)`. Si la cola está llena durante todo el timeout de **encolado** → `503` (“backpressure”). Si entra pero no termina en el timeout de **ejecución** → `503` (“timeout”).  
- **Métricas internas**: contadores de `submitted/completed/rejected`, `avg_wait/avg_run`, `workers.busy` y tamaños de colas. La ruta `/metrics` consolida todo.  
- **Seguridad mínima**: sanitización de nombres de archivo (`/createfile`, `/deletefile`) para evitar traversal.  
- **Determinismo**: `matrixmul` usa PRNG con `seed` y devuelve hash, útil para validar resultados sin transferir matrices grandes.

---

## Roadmap inmediato

- **Endpoints IO-bound**: `sortfile`, `wordcount`, `grep`, `compress (gzip/xz)`, `hashfile`.  
- **Modelo de Jobs** (colas internas con polling y `/jobs/{submit,status,result,cancel}`) para tareas que exceden la interacción directa HTTP/1.0.  
- **Pruebas de desempeño**: perfiles de p50/p95/p99 y throughput con distintas configuraciones (`WORKERS_*`, `QUEUE_*`).

---

## Licencia / uso

Código para fines educativos (curso de Sistemas Operativos). Se anima a modificar, extender y documentar resultados de desempeño y escalabilidad en el informe final.
