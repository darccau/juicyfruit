# Juicyfruit

[English](README.md) · [Português do Brasil](README.pt-BR.md)

Juicyfruit es una infraestructura local-first para ingeniería asistida por IA de forma durable y portable.

Los agentes y chats son temporales. La memoria del proyecto, el estado de tareas, las skills, reglas, templates y el contexto no deberían serlo. Juicyfruit mantiene esa capa durable en archivos controlados por el desarrollador y renderiza las piezas correctas hacia el target de agente que elijas explícitamente.

## Qué Entrega Juicyfruit

- Setup portable para Codex, Claude Code, Cursor, Antigravity/GCA, Devin, Gemini CLI, Via y targets genéricos basados en directorios.
- Memoria durable para conocimiento de usuario y proyecto fuera del chat actual.
- Paquetes de contexto determinísticos para una tarea específica.
- Skills, reglas, personas, sub-agentes, templates, scripts, assets y referencias portables.
- Runtime durable de tareas con tareas, dependencias, dueños, sesiones, checkpoints, bloqueos, validaciones y próximas acciones.
- Fuentes de paquetes de solo lectura, resolución de dependencias, lockfiles, instalación transaccional, status, diff, update y uninstall seguro.
- Operación local-first: el estado sigue siendo legible, versionable, recuperable e inspeccionable en la máquina del desarrollador.

## Cómo Funciona

```text
estado canónico de Juicyfruit
        |
        v
fuentes, paquetes, memoria, tareas, contexto
        |
        v
adaptadores de target
        |
        v
Codex / Claude Code / Cursor / Antigravity / Devin / Gemini CLI / Via / genérico
```

Directorios de provider como `.codex/`, `.claude/`, `.cursor/`, `.gemini/`, `.devin/`, `.via/`, `.agents/` y `.juicyfruit/` son salidas de adaptadores. No son el tablero canónico, la memoria ni la fuente de verdad.

## Conceptos Centrales

### Memoria

La memoria responde: "qué sabemos?"

Guarda decisiones durables, hechos, convenciones, aprendizajes e historial relevante en dos alcances:

- `~/.juicyfruit/memory/` para conocimiento que aplica entre proyectos.
- `<repo>/.juicyfruit/memory/` para conocimiento que solo aplica a un repositorio.

La memoria es append-only por diseño. Los journals crecen con el tiempo, las notas se escriben una vez y los índices derivados se pueden regenerar. La búsqueda funciona offline por palabras clave. Embeddings locales opcionales con Ollama mejoran el ranking cuando están disponibles y vuelven a palabras clave cuando no lo están.

### Contexto

El contexto responde: "qué debe cargar esta tarea ahora?"

Los proyectos pueden declarar tópicos de contexto en `.juicyfruit/config.yaml`. Una tarea puede referenciar tópicos y archivos. `juicyfruit context build JCF-...` devuelve un paquete determinístico `juicyfruit.context/v1` con fuentes agrupadas y una estimación aproximada de tokens, para que los agentes carguen material relevante en vez de redescubrir el proyecto en cada sesión.

### Skills

Las skills son guías portables de capacidad. Cada skill vive en `skills/<nombre>/SKILL.md`, con referencias, scripts y assets opcionales cargados solo cuando hacen falta. Las skills hacen que el comportamiento del agente sea enseñable sin volcar todos los detalles en cada prompt.

### Reglas

Las reglas son restricciones y estándares siempre activos. Viven en `rules/` y los adaptadores las renderizan hacia cada target seleccionado. Usa reglas para comportamientos que deben aplicar ampliamente; usa skills para procedimientos específicos de una tarea.

### Personas

Las personas son archivos livianos de voz y preferencias. Viven en `personas/` y pueden sembrarse en targets sin sobrescribir ediciones locales del usuario en updates futuros.

### Templates

Los templates describen la forma deseada de un artefacto de salida: voz, estructura, tamaño, formato y restricciones. Juicyfruit provee el schema y los comandos; los equipos crean y empaquetan sus propios templates.

Al crear un template, pregunta o informa el tipo, para que sirve y el formato de
salida. El formato es abierto: los targets conocidos son sugerencias, y un
proyecto puede definir un target de texto puro, archivo customizado o renderer
especifico. Los templates en blanco se escriben en el repositorio en
`templates/<name>/TEMPLATE.md`, no en el perfil de usuario, para poder
empaquetarlos o exportarlos despues.

### Paquetes Y Fuentes

Los paquetes agrupan contenido canónico en unidades instalables versionadas. Un paquete declara sus targets soportados, dependencias, conflictos y rutas de contenido. Las fuentes son catálogos de solo lectura, locales, Git o archives. Instalar paquetes escribe en adaptadores de target, nunca en la fuente.

### Targets Y Adaptadores

Un target es un ambiente explícito de agente, como `codex`, `claude-code`, `cursor`, `antigravity`, `devin`, `gemini-cli`, `via`, `generic` o `dsh`. Un adaptador mapea contenido canónico de Juicyfruit al layout de directorios de ese target. La integración DSH requiere DSH `0.1.5-rc.3` o posterior.

`juicyfruit setup --all` instala solamente los targets declarados por los paquetes oficiales descubiertos que ya están configurados en el alcance local seleccionado. No instala en todos los adapters incorporados solo porque Juicyfruit sabe que existen.

### Runtime De Tareas

El Runtime de Tareas responde: "qué estamos haciendo ahora?"

Los proyectos mantienen estado durable de trabajo en `.juicyfruit/` y estado local de máquina en `.juicyfruit/runtime/`. Las entidades centrales son:

- Run: un objetivo mayor o unidad de orquestación.
- Task: una unidad durable de trabajo con criterios de aceptación y dependencias.
- Session: una instancia temporal de worker usando un adapter/provider soportado.

`READY` es derivado, no persistido. Una tarea está lista cuando está en `BACKLOG`, no tiene dueño, no está bloqueada ni terminal, y todas las dependencias están `DONE`.

Los flujos de desarrollo mayores terminan con una verificación de cierre:
revisar el conjunto de READMEs para detectar comportamiento o notas de setup
desactualizados y revisar los elementos alterados para ver si algo debe ir a la
memoria antes de reportar la tarea master como completa.

## Instalación

macOS y Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.ps1 | iex
```

El instalador descarga la release más reciente de GitHub para la plataforma actual, valida el checksum SHA-256, instala en `~/.juicyfruit/bin` y actualiza el PATH del shell actual. Define `JUICYFRUIT_INSTALL_DIR` para elegir otro destino.

### Instalación en DeepSeek Harness (DSH)

La integración con DeepSeek Harness opera en dos capas complementarias:

1. **Plugin Web (Pestaña Juicyfruit y Kanban)**: Registra el plugin Cordis de Juicyfruit en el perfil web de DSH apuntando a la ruta del paquete o repositorio local:
   ```sh
   # Agrega el plugin al perfil web de DSH
   dsh plugin --profile web add /ruta/a/juicyfruit/packages/juicyfruit-plugin

   # Inicia la interfaz web de DSH
   dsh --profile web
   ```
2. **Skills y Adaptador de Agentes (`juicyfruit setup --dsh`)**: Renderiza las skills oficiales (incluida `juicyfruit-worker`), reglas e instrucciones en `.dsh/` para que los agentes dentro de DSH descubran y ejecuten tareas de forma autónoma:
   ```sh
   # Instala skills y reglas en ~/.dsh (o a nivel de proyecto com --scope project)
   juicyfruit setup --dsh
   ```

Ambos pueden usarse juntos para la experiencia completa (interfaz gráfica en DSH Web + ejecución autónoma de workers en agentes DSH).

## Setup De Targets

Instala paquetes oficiales en todos los targets configurados y declarados por esos paquetes:

```sh
juicyfruit setup --all
```

Instala solo targets seleccionados:

```sh
juicyfruit setup --codex
juicyfruit setup --cursor --gemini-cli
juicyfruit setup --devin --via
juicyfruit setup --claude-code
juicyfruit setup --antigravity
juicyfruit setup --generic
juicyfruit setup --dsh
```

Usa `--scope project` para instalar en el proyecto actual en vez del alcance de usuario, y `--force` para sobrescribir archivos administrados modificados localmente.

## Comandos Comunes

```sh
juicyfruit init source ./mi-fuente
juicyfruit source add canonical --local ./mi-fuente
juicyfruit package list
juicyfruit package install android-core --target codex --scope user
juicyfruit package status --target codex
juicyfruit update --dry-run
juicyfruit upgrade --check
juicyfruit template new email-cliente --kind email --description "Use para email de cliente. No para chat interno." --format-target email
juicyfruit template validate email-cliente
```

```sh
juicyfruit memory setup --target codex
juicyfruit memory add "documente el runtime de tareas"
juicyfruit memory add --kind note --title "Decision de runtime" "las tareas son canonicas"
juicyfruit memory list --since 30d
juicyfruit memory search "handoff de tarea"
juicyfruit memory digest --since 14d
juicyfruit memory capture
```

```sh
juicyfruit task create --title "Implementar soporte de reconnect"
juicyfruit task ready
juicyfruit session start --adapter codex
juicyfruit task claim JCF-... --adapter codex --session SES-...
juicyfruit task start JCF-...
juicyfruit task checkpoint JCF-... --completed "protocolo" --current "tests"
juicyfruit task verify JCF-...
juicyfruit task done JCF-...
juicyfruit task resume JCF-...
juicyfruit context build JCF-...
```

La mayoría de los comandos de runtime soporta `--json` para agentes y herramientas.

## Estructura Del Repositorio

```text
juicyfruit/
├── .github/             # CI, actualizaciones de dependencias y templates del repo
├── adapters/            # Adaptadores de instalacion y renderizado por agente
├── agents/              # Sub-agentes canonicos
├── cmd/juicyfruit/         # Entrada de la CLI Go multiplataforma
├── docs/                # Documentacion de arquitectura y migraciones
├── packages/            # Bundles instalables de contenido canonico
├── personas/            # Seeds de voz y preferencias durables
├── profiles/            # Selecciones reproducibles personales, trabajo y proyecto
├── rules/               # Reglas canonicas siempre activas
├── schemas/             # Contratos JSON versionados
└── skills/              # Skills portables canonicas
```

## Notas De Arquitectura

- [Adaptadores De Agente](docs/architecture/agent-adapters.md)
- [Agentes](docs/architecture/agents.md)
- [Motor De Contexto](docs/architecture/context-engine.md)
- [Memoria](docs/architecture/memory.md)
- [Personas](docs/architecture/personas.md)
- [Reglas](docs/architecture/rules.md)
- [Skills](docs/architecture/skills.md)
- [Runtime De Tareas](docs/architecture/task-runtime.md)
- [Templates](docs/architecture/templates.md)

## Desarrollo

La CLI esta escrita en Go y usa la version declarada en `go.mod`.

```sh
go build ./...
go vet ./...
go test ./...

# Ejecutar la CLI sin instalarla
go run ./cmd/juicyfruit --help

# Compilar un ejecutable local
go build -o juicyfruit ./cmd/juicyfruit
```
