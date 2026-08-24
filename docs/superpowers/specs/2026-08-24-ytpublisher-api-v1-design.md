# YTPublisher API — Diseño v1

**Fecha:** 2026-08-24
**Estado:** Aprobado para pasar a plan de implementación

## Contexto y objetivo

YTPublisher API es un side-project personal (2-4h/semana disponibles) cuyo
objetivo es generar ingresos pasivos vía pago por uso. Es una API pura (sin
frontend en v1) pensada para que devs/agencias la integren para generar
contenido de publicación de videos de YouTube (título, descripción
estructurada, tags, videos relacionados) usando contexto real del canal en
vez de ser un wrapper simple de prompt a LLM.

## Scope v1

**Incluido:**
- Input: `channel_id`, tema del video, notas/transcript opcional, idioma,
  links, menciones, tono deseado.
- Fetch de los últimos videos públicos del canal vía YouTube Data API
  (API key, sin OAuth).
- Detección heurística de patrones de estilo del canal (títulos, tags,
  estructura de descripción).
- Embeddings de videos existentes para sugerir videos relacionados reales
  por similitud semántica.
- Generación con Claude de un JSON estructurado: título, descripción (hook,
  cuerpo, timestamps, links, menciones, hashtags), tags, videos
  relacionados.
- Validación de reglas de YouTube: título ≤100 caracteres, tags ≤500
  caracteres en total, hook dentro de los primeros ~125 caracteres.
- Cache del análisis de estilo del canal (no re-consultar todo el
  historial en cada request).
- Auth por API key propia del servicio, pago por uso vía Stripe (metered
  billing).

**Fuera de scope v1 (v2):**
- OAuth y YouTube Analytics (performance real de videos).
- Frontend/UI para usuario final.
- Procesamiento asíncrono (queue/jobs) — v1 es síncrono.
- Vector DB dedicada — v1 usa pgvector sobre Postgres.

## Decisiones clave (y por qué)

| Decisión | Elección | Motivo |
|---|---|---|
| Hosting | VPS/Fly.io/Railway o AWS (a definir el proveedor exacto, no afecta arquitectura) | Servicio Go long-running; evita las limitaciones de serverless para cache en memoria y jobs |
| Embeddings | Voyage AI | Claude no ofrece API de embeddings; Voyage es la recomendación oficial de Anthropic para RAG |
| Almacenamiento vectorial | pgvector sobre Postgres (Supabase) | Volumen esperado bajo-medio en v1; evita sobre-ingeniería de una vector DB dedicada, migrable después |
| Billing | Pago por uso + Stripe (metered billing) | Encaja con el patrón "devs/agencias integrando por API"; más simple que tiers con features diferenciadas |
| Ownership de API keys externas | Todas del servicio (YouTube, Claude, Voyage) | Onboarding simple para el cliente; el costo variable se absorbe en el precio cobrado |
| Modo de respuesta | Síncrono simple | Pipeline cacheado responde en pocos segundos; evita la complejidad de una cola de trabajo en v1 |
| Detección de estilo del canal | Heurística estadística propia (no LLM) | Barata, rápida, determinística, cacheable — insumo compacto para el prompt de generación |
| Gobernanza de cuota de YouTube | Cache agresivo + contador diario en DB + 429 antes de agotar cuota | La cuota (10k unidades/día) es un recurso compartido entre todos los clientes del servicio |

## Arquitectura

Monolito modular en Go, un solo binario, sin microservicios. Router
liviano (`chi`). Cada integración externa vive detrás de una interfaz
definida por quien la consume, para poder testear con mocks y cambiar de
proveedor sin tocar el orquestador.

```
ytpublisher-api/
  cmd/api/                  # main.go — wiring, arranque del server
  internal/
    api/                    # HTTP handlers, middleware (auth, rate-limit), routing
    youtube/                # cliente YouTube Data API (interfaz + impl real)
    styleanalysis/          # heurística de estilo del canal (funciones puras)
    embeddings/             # cliente Voyage AI + lógica de similitud coseno
    llm/                    # cliente Claude, construcción de prompt, parsing structured output
    rules/                  # validación de reglas de YouTube (funciones puras)
    generation/             # orquestador: junta todo lo anterior para /generate
    billing/                # tracking de uso, integración Stripe
    storage/                # repos de Postgres (pgx)
    config/                 # carga de env vars
  migrations/               # SQL migrations (goose)
```

### Flujo de un request a `POST /v1/generate`

1. Middleware de auth valida API key → identifica cliente.
2. Se busca `channel_style_cache` para el `channel_id`; si no existe o
   expiró (TTL configurable, ej. 48h) → se consulta YouTube (respetando el
   contador de cuota diaria) y se recalcula el estilo.
3. Se buscan embeddings cacheados de los videos del canal; los que falten
   (videos nuevos desde el último fetch) se calculan con Voyage.
4. Se arma el prompt para Claude combinando: resumen de estilo + input del
   usuario (tema, notas, tono, links, menciones) + candidatos de "related
   videos" ya filtrados por similitud coseno.
5. Se llama a Claude pidiendo *structured output* (JSON forzado por
   schema, no parseo de texto libre).
6. El módulo `rules/` valida longitudes/estructura; si falla, se hace
   **un** intento de reparación (se le pide a Claude corregir el campo
   específico); si vuelve a fallar, se ajusta programáticamente
   (truncar) y se marca un warning en la respuesta — nunca se falla en
   silencio.
7. Se registra el uso (unidades de YouTube, tokens de Claude, llamadas a
   Voyage) para billing.
8. Se responde.

## Modelo de datos (Postgres + pgvector en Supabase)

```sql
-- Clientes de la API (devs/agencias que consumen el servicio)
api_clients (
  id, name, email, api_key_hash, stripe_customer_id,
  created_at, is_active
)

-- Registro de cada request para billing y auditoría
usage_events (
  id, client_id FK, request_id, endpoint,
  youtube_units_used, embedding_calls, llm_input_tokens, llm_output_tokens,
  estimated_cost_usd, created_at
)

-- Historial de generaciones (debugging, soporte, futura analytics en v2)
generation_requests (
  id, client_id FK, channel_id, input_params_json,
  output_json, validation_warnings_json, created_at
)

-- Cache de videos del canal + su embedding (evita re-fetch y re-embed)
channel_videos (
  channel_id, video_id, title, description, tags_json,
  published_at, embedding vector(1024),  -- dimensión según modelo Voyage elegido
  fetched_at,
  PRIMARY KEY (channel_id, video_id)
)

-- Cache del análisis de estilo, con TTL explícito
channel_style_cache (
  channel_id PK, style_summary_json, video_count_analyzed,
  computed_at, expires_at
)

-- Contador de cuota diaria de YouTube (recurso compartido entre todos los clientes)
youtube_quota_usage (
  date PK, units_used
)
```

Notas:
- Búsqueda de related videos: `ORDER BY embedding <=> $query_embedding
  LIMIT k`; índice HNSW se agrega después si el volumen lo justifica.
- `youtube_quota_usage` se incrementa atómicamente
  (`UPDATE ... SET units_used = units_used + N WHERE date = today`) — sin
  necesidad de Redis a esta escala.
- `video_count_analyzed` permite marcar "low confidence" para canales con
  pocos videos.

## Librerías y herramientas

| Necesidad | Elección | Por qué |
|---|---|---|
| HTTP router | `chi` | Idiomático, compatible con `net/http` stdlib, sin magia |
| Acceso a Postgres | `pgx` + `pgvector-go` | Soporte nativo de tipos Postgres incluido `vector` |
| Migraciones | `goose` | SQL plano, sin DSL propio |
| YouTube Data API | `google.golang.org/api/youtube/v3` | Cliente oficial de Google |
| Claude (LLM) | SDK oficial de Anthropic para Go | Soporta structured output / tool-use nativamente |
| Voyage AI | Cliente REST propio | Un solo endpoint HTTP, no justifica SDK |
| Stripe | `stripe-go` (oficial) | Metered billing, customers, subscriptions |
| Config | `envconfig` o parseo manual | Suficiente para el volumen de config de v1 |
| Testing | `testing` stdlib + `testify` | Estándar del ecosistema Go |

No se necesita cola de trabajo en v1 (flujo síncrono). Si en v2 se pasa a
async, evaluar `river` (cola sobre Postgres) antes de sumar infra nueva.

## Riesgos técnicos y mitigación

| Riesgo | Mitigación |
|---|---|
| Cuota de YouTube compartida (10k unidades/día) | Cache con TTL + contador atómico + `429` claro antes de agotarla |
| Canal con pocos videos (patrón no confiable) | Umbral mínimo (ej. <5 videos) → estilo genérico + flag `style_confidence: "low"` |
| Costo variable del LLM | Tracking de tokens por request + tope máximo de tokens de salida |
| Output de Claude no cumple el JSON schema | Structured output forzado + 1 reintento de reparación acotado + ajuste programático como último recurso |
| Canal privado/inexistente/ID inválido | Errores tipados (`ErrChannelNotFound`, `ErrChannelPrivate`) → 4xx claro, no 500 genérico |
| Voyage AI caído o rate-limited | Se genera el resto del contenido igual; `related_videos` se omite o marca `unavailable` |
| Concurrencia sobre el contador de cuota | Incremento atómico vía SQL (`UPDATE ... SET x = x + N`), sin locks explícitos |

## Roadmap por sesiones (2-4h c/u)

| # | Sesión | Entregable visible |
|---|---|---|
| 1 | Scaffold: módulo Go, `chi`, health check, config, conexión a Supabase, migración inicial (`api_clients`) | Server local corriendo + deploy inicial "hello world" |
| 2 | Auth por API key + `usage_events` + comando para emitir keys | Endpoint protegido: 401 sin key, 200 con key válida |
| 3 | Cliente YouTube Data API + `channel_videos` + `youtube_quota_usage` | Endpoint interno que trae y cachea videos reales de un canal |
| 4 | `styleanalysis/` (heurística) + tests + `channel_style_cache` con TTL | Dado un `channel_id`, devuelve estilo detectado cacheado |
| 5 | Cliente Voyage + embeddings + búsqueda por similitud coseno | Endpoint que devuelve videos reales similares a un tema |
| 6 | `rules/` (validación) + tests con casos límite | Funciones puras testeadas (título 101 vs 100 chars, etc.) |
| 7 | Orquestador `generation/` + cliente Claude + prompt + structured output + reparación | **`POST /v1/generate` end-to-end (happy path)** — hito central |
| 8 | Billing: Stripe, reporte de uso metered, estimación de costo | Cliente de prueba genera contenido, uso visible en Stripe test mode |
| 9 | Hardening: errores, logging estructurado, tests de integración con mocks | Suite cubriendo los casos de la tabla de riesgos |
| 10 | Docs para integradores, deploy a producción, smoke test, alerta de cuota | API pública documentada y deployada — v1 lista |

Total estimado: ~10 sesiones (20-40h). `/v1/generate` funciona end-to-end
desde la sesión 7; las sesiones 8-10 suman monetización y robustez sobre
una base ya funcional.

## Checklist de configuración/cuentas previas

- [ ] Google Cloud Console: proyecto, habilitar YouTube Data API v3, API key
- [ ] Anthropic: cuenta + API key (Claude)
- [ ] Voyage AI: cuenta + API key
- [ ] Supabase: proyecto, extensión `pgvector` habilitada, connection string
- [ ] Stripe: cuenta test mode, producto + price para metered billing
- [ ] Hosting: cuenta en Fly.io/Railway o AWS (definir cuál antes de sesión 1)
- [ ] Go 1.22+ instalado localmente
- [ ] (Opcional) dominio propio para la URL base de la API
