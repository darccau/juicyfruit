# Juicyfruit

[English](README.md) · [Español](README.es.md)

O Juicyfruit é uma infraestrutura local-first para engenharia assistida por IA de forma durável e portável.

Agentes e chats são temporários. Memória de projeto, estado de tarefas, skills, regras, templates e contexto não deveriam ser. O Juicyfruit mantém essa camada durável em arquivos controlados pelo desenvolvedor e renderiza as partes certas para o target de agente que você escolher explicitamente.

## O Que O Juicyfruit Entrega

- Setup portável para Codex, Claude Code, Cursor, Antigravity/GCA, Devin, Gemini CLI, Via e targets genéricos baseados em diretório.
- Memória durável para conhecimento de usuário e projeto fora do chat atual.
- Pacotes de contexto determinísticos para uma tarefa específica.
- Skills, regras, personas, sub-agentes, templates, scripts, assets e referências portáveis.
- Runtime durável de tarefas com tarefas, dependências, donos, sessões, checkpoints, bloqueios, validações e próximas ações.
- Fontes de pacote somente leitura, resolução de dependências, lockfiles, instalação transacional, status, diff, update e uninstall seguro.
- Operação local-first: o estado permanece legível, versionável, recuperável e inspecionável na máquina do desenvolvedor.

## Como Funciona

```text
estado canônico do Juicyfruit
        |
        v
fontes, pacotes, memória, tarefas, contexto
        |
        v
adaptadores de target
        |
        v
Codex / Claude Code / Cursor / Antigravity / Devin / Gemini CLI / Via / genérico
```

Diretórios de provider como `.codex/`, `.claude/`, `.cursor/`, `.gemini/`, `.devin/`, `.via/`, `.agents/` e `.juicyfruit/` são saídas dos adaptadores. Eles não são o quadro canônico, a memória ou a fonte da verdade.

## Conceitos Centrais

### Memória

Memória responde: "o que sabemos?"

Ela guarda decisões duráveis, fatos, convenções, aprendizados e histórico relevante em dois escopos:

- `~/.juicyfruit/memory/` para conhecimento que vale entre projetos.
- `<repo>/.juicyfruit/memory/` para conhecimento que só faz sentido em um repositório.

A memória é append-only por desenho. Journals crescem com o tempo, notas são escritas uma vez e índices derivados podem ser regenerados. A busca funciona offline por palavras-chave. Embeddings locais opcionais via Ollama melhoram o ranking quando disponíveis e voltam para palavras-chave quando indisponíveis.

### Contexto

Contexto responde: "o que esta tarefa deve carregar agora?"

Projetos podem declarar tópicos de contexto em `.juicyfruit/config.yaml`. Uma tarefa pode referenciar tópicos e arquivos. `juicyfruit context build JCF-...` retorna um pacote determinístico `juicyfruit.context/v1` com fontes agrupadas e estimativa aproximada de tokens, para que agentes carreguem material relevante em vez de redescobrir o projeto a cada sessão.

### Skills

Skills são guias portáveis de capacidade. Cada skill vive em `skills/<nome>/SKILL.md`, com referências, scripts e assets opcionais carregados apenas quando necessário. Skills tornam o comportamento do agente ensinável sem despejar todos os detalhes em todo prompt.

### Regras

Regras são restrições e padrões sempre ativos. Elas vivem em `rules/` e são renderizadas pelos adaptadores para cada target selecionado. Use regras para comportamentos que devem valer amplamente; use skills para procedimentos específicos de tarefa.

### Personas

Personas são arquivos leves de voz e preferências. Elas vivem em `personas/` e podem ser semeadas nos targets sem sobrescrever edições locais do usuário em updates futuros.

### Templates

Templates descrevem a forma desejada de um artefato de saída: voz, estrutura, tamanho, formato e restrições. O Juicyfruit fornece o schema e os comandos; equipes criam e empacotam seus próprios templates.

Ao criar um template, pergunte ou informe o tipo, para que ele serve e o formato
de saída. O formato é aberto: targets conhecidos são sugestões, e um projeto
pode definir um target de texto puro, arquivo customizado ou renderer
específico. Templates em branco são gravados no repositório em
`templates/<name>/TEMPLATE.md`, não no perfil do usuário, para poderem ser
empacotados ou exportados depois.

### Pacotes E Fontes

Pacotes agrupam conteúdo canônico em unidades instaláveis versionadas. Um pacote declara seus targets suportados, dependências, conflitos e caminhos de conteúdo. Fontes são catálogos somente leitura, locais, Git ou archives. Instalar pacotes escreve nos adaptadores de target, nunca na fonte.

### Targets E Adaptadores

Um target é um ambiente explícito de agente, como `codex`, `claude-code`, `cursor`, `antigravity`, `devin`, `gemini-cli`, `via`, `generic` ou `dsh`. Um adaptador mapeia conteúdo canônico do Juicyfruit para o layout de diretórios daquele target. A integração DSH requer DSH `0.1.5-rc.3` ou mais recente.

`juicyfruit setup --all` instala apenas os targets que foram declarados pelos pacotes oficiais descobertos e já estão configurados no escopo local selecionado. Ele não instala em todo adapter embutido só porque o Juicyfruit sabe que ele existe.

### Runtime De Tarefas

Runtime de Tarefas responde: "o que estamos fazendo agora?"

Projetos mantêm estado durável de trabalho em `.juicyfruit/` e estado local de máquina em `.juicyfruit/runtime/`. As entidades centrais são:

- Run: um objetivo maior ou unidade de orquestração.
- Task: uma unidade durável de trabalho com critérios de aceite e dependências.
- Session: uma instância temporária de worker usando um adapter/provider suportado.

`READY` é derivado, não persistido. Uma tarefa está pronta quando está em `BACKLOG`, não tem dono, não está bloqueada ou terminal, e todas as dependências estão `DONE`.

Fluxos de desenvolvimento maiores terminam com uma checagem de fechamento:
revisar o conjunto de READMEs em busca de comportamento ou notas de setup
desatualizados e revisar os itens alterados para ver se algo precisa ir para a
memória antes de reportar a tarefa master como completa.

## Instalação

macOS e Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.ps1 | iex
```

O instalador baixa a release mais recente do GitHub para a plataforma atual, valida o checksum SHA-256, instala em `~/.juicyfruit/bin` e atualiza o PATH do shell atual. Defina `JUICYFRUIT_INSTALL_DIR` para escolher outro destino.

### Instalação no DeepSeek Harness (DSH)

A integração com o DeepSeek Harness funciona em duas camadas complementares:

1. **Plugin Web (Aba Juicyfruit e Kanban)**: Registre o plugin Cordis do Juicyfruit no perfil web do DSH apontando para o diretório do pacote ou clone local:
   ```sh
   # Adiciona o plugin web ao perfil do DSH
   dsh plugin --profile web add /caminho/para/juicyfruit/packages/juicyfruit-plugin

   # Inicia a interface web do DSH
   dsh --profile web
   ```
2. **Skills e Adaptador de Agentes (`juicyfruit setup --dsh`)**: Renderiza as skills oficiais (incluindo `juicyfruit-worker`), regras e contexto em `.dsh/` para que agentes executando dentro do DSH descubram e operem tarefas autonomamente:
   ```sh
   # Instala skills e regras no target DSH (~/.dsh ou no projeto com --scope project)
   juicyfruit setup --dsh
   ```

Ambos podem ser usados juntos para a experiência completa (interface visual no DSH Web + execução de workers por agentes no DSH).

## Setup De Targets

Instale pacotes oficiais em todos os targets configurados e declarados por esses pacotes:

```sh
juicyfruit setup --all
```

Instale apenas targets selecionados:

```sh
juicyfruit setup --codex
juicyfruit setup --cursor --gemini-cli
juicyfruit setup --devin --via
juicyfruit setup --claude-code
juicyfruit setup --antigravity
juicyfruit setup --generic
juicyfruit setup --dsh
```

Use `--scope project` para instalar no projeto atual em vez do escopo de usuário, e `--force` para sobrescrever arquivos gerenciados modificados localmente.

## Comandos Comuns

```sh
juicyfruit init source ./minha-fonte
juicyfruit source add canonical --local ./minha-fonte
juicyfruit package list
juicyfruit package install android-core --target codex --scope user
juicyfruit package status --target codex
juicyfruit update --dry-run
juicyfruit upgrade --check
juicyfruit template new email-cliente --kind email --description "Use para email de cliente. Nao para chat interno." --format-target email
juicyfruit template validate email-cliente
```

```sh
juicyfruit memory setup --target codex
juicyfruit memory add "documentei o runtime de tarefas"
juicyfruit memory add --kind note --title "Decisao de runtime" "tarefas sao canonicas"
juicyfruit memory list --since 30d
juicyfruit memory search "handoff de tarefa"
juicyfruit memory digest --since 14d
juicyfruit memory capture
```

```sh
juicyfruit task create --title "Implementar suporte a reconnect"
juicyfruit task ready
juicyfruit session start --adapter codex
juicyfruit task claim JCF-... --adapter codex --session SES-...
juicyfruit task start JCF-...
juicyfruit task checkpoint JCF-... --completed "protocolo" --current "testes"
juicyfruit task verify JCF-...
juicyfruit task done JCF-...
juicyfruit task resume JCF-...
juicyfruit context build JCF-...
```

A maioria dos comandos de runtime suporta `--json` para agentes e ferramentas.

## Estrutura Do Repositório

```text
juicyfruit/
├── .github/             # CI, atualizacoes de dependencias e templates do repo
├── adapters/            # Adaptadores de instalacao e renderizacao por agente
├── agents/              # Sub-agentes canonicos
├── cmd/juicyfruit/         # Entrada da CLI Go multiplataforma
├── docs/                # Documentacao de arquitetura e migracoes
├── packages/            # Bundles instalaveis de conteudo canonico
├── personas/            # Seeds de voz e preferencias duraveis
├── profiles/            # Selecoes reproduziveis pessoais, trabalho e projeto
├── rules/               # Regras canonicas sempre ativas
├── schemas/             # Contratos JSON versionados
└── skills/              # Skills portaveis canonicas
```

## Notas De Arquitetura

- [Adaptadores De Agente](docs/architecture/agent-adapters.md)
- [Agentes](docs/architecture/agents.md)
- [Motor De Contexto](docs/architecture/context-engine.md)
- [Memória](docs/architecture/memory.md)
- [Personas](docs/architecture/personas.md)
- [Regras](docs/architecture/rules.md)
- [Skills](docs/architecture/skills.md)
- [Runtime De Tarefas](docs/architecture/task-runtime.md)
- [Templates](docs/architecture/templates.md)

## Desenvolvimento

A CLI e escrita em Go e usa a versao declarada em `go.mod`.

```sh
go build ./...
go vet ./...
go test ./...

# Executar a CLI sem instalar
go run ./cmd/juicyfruit --help

# Compilar um executavel local
go build -o juicyfruit ./cmd/juicyfruit
```
