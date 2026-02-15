# goutbox

Uma biblioteca Go simples e eficiente que implementa o **Outbox Pattern** para garantir a publicação confiável de eventos.

## O que é o Outbox Pattern?

O Outbox Pattern é um padrão de design utilizado para garantir a **consistência na publicação de eventos** em sistemas distribuídos. Ele resolve o problema clássico de "dual write", onde você precisa:

1. Persistir dados no banco de dados
2. Publicar um evento para um message broker (Kafka, RabbitMQ, etc.)

Sem o Outbox Pattern, se a publicação do evento falhar após salvar os dados, você perde a garantia de entrega. Com goutbox, os eventos que falharam são armazenados e reprocessados automaticamente até serem publicados com sucesso.

### Benefícios

- **Garantia de entrega**: Eventos são persistidos e reprocessados em caso de falha
- **Retry automático**: Tentativas automáticas com número configurável de retries
- **Simples de usar**: API minimalista e fácil de integrar
- **Extensível**: Implemente sua própria Store para diferentes bancos de dados

## Instalação

```bash
go get github.com/IsaacDSC/goutbox
```

## Uso Básico

### 1. Criar uma Store

A biblioteca inclui uma implementação em memória para desenvolvimento/testes:

```go
import "github.com/IsaacDSC/goutbox/stores"

store := stores.NewMemStore()
```

Para produção, implemente a interface `Store` com seu banco de dados preferido.

### 2. Inicializar o Outbox

```go
import "github.com/IsaacDSC/goutbox"

outbox := goutbox.New(store)
```

Ou com configurações personalizadas:

```go
outbox := goutbox.New(store, goutbox.WithLoopInterval(30*time.Second))
```

### 3. Iniciar o Worker de Reprocessamento

O worker roda em background e reprocessa eventos que falharam:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go outbox.Start(ctx, func(ctx context.Context, eventName string, payload any) error {
    // Sua lógica de publicação aqui
    return publishToKafka(ctx, eventName, payload)
})
```

### 4. Publicar Eventos

Use o método `Do` para publicar eventos com garantia:

```go
err := outbox.Do(ctx, "user.created", userData,
    func(ctx context.Context, eventName string, payload any) error {
        return publishToKafka(ctx, eventName, payload)
    },
)
```

Ou com número personalizado de tentativas:

```go
err := outbox.Do(ctx, "user.created", userData,
    func(ctx context.Context, eventName string, payload any) error {
        return publishToKafka(ctx, eventName, payload)
    },
    goutbox.WithMaxAttempts(5),
)
```

Se a publicação falhar, o evento é automaticamente salvo na Store e será reprocessado pelo worker.

## Exemplo Completo

```go
package main

import (
    "context"
    "log"

    "github.com/IsaacDSC/goutbox"
    "github.com/IsaacDSC/goutbox/stores"
)

func main() {
    // Inicializa a store
    store := stores.NewMemStore()
    defer store.Close()

    // Cria o serviço de outbox
    outbox := goutbox.New(store)

    // Inicia o worker em background
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go outbox.Start(ctx, publisher)

    // Publica um evento
    err := outbox.Do(ctx, "order.created", map[string]any{
        "order_id": "12345",
        "amount":   99.90,
    }, publisher)

    if err != nil {
        log.Printf("Erro ao processar evento: %v", err)
    }
}

func publisher(ctx context.Context, eventName string, payload any) error {
    // Implemente sua lógica de publicação aqui
    // Ex: Kafka, RabbitMQ, SQS, etc.
    log.Printf("Publicando evento: %s, payload: %v", eventName, payload)
    return nil
}
```

## Interface Store

Para implementar uma Store customizada, implemente a interface:

```go
type Store interface {
    Create(ctx context.Context, task Task) error
    GetTasksWithError(ctx context.Context) ([]Task, error)
    UpdateTaskError(ctx context.Context, task Task) error
    DiscardTask(ctx context.Context, key string, isOk bool) error
}
```

## PostgreSQL Store (Persistência)

Para ambientes de produção, utilize o módulo PostgreSQL que oferece persistência durável e suporte a múltiplas instâncias.

### Instalação

O PostgreSQL store é um módulo separado para manter o core leve:

```bash
# Apenas o core (sem dependências pesadas)
go get github.com/IsaacDSC/goutbox

# PostgreSQL store (inclui driver pq)
go get github.com/IsaacDSC/goutbox/stores/postgres
```

### Uso com PostgreSQL

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "time"

    "github.com/IsaacDSC/goutbox"
    "github.com/IsaacDSC/goutbox/stores/postgres"
    _ "github.com/lib/pq"
)

func main() {
    // Conecta ao PostgreSQL
    db, err := sql.Open("postgres", "postgres://user:pass@localhost:5432/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Cria a store (migrations são executadas automaticamente)
    store, err := postgres.NewPostgresStore(db,
        postgres.WithRetryInterval(1*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Usa normalmente com o outbox
    outbox := goutbox.New(store)
    
    ctx := context.Background()
    go outbox.Start(ctx, publisher)

    // ...
}
```

### Opções do PostgreSQL Store

| Opção | Descrição | Default |
|-------|-----------|---------|
| `WithRetryInterval(d time.Duration)` | Tempo mínimo antes de retentar uma task | 500ms |
| `WithSkipMigration()` | Desabilita migrations automáticas | false |

```go
// Com migrations gerenciadas externamente (Flyway, golang-migrate, etc.)
store, err := postgres.NewPostgresStore(db,
    postgres.WithRetryInterval(2*time.Second),
    postgres.WithSkipMigration(),
)
```

### Docker Compose

O projeto inclui um `docker-compose.yml` para desenvolvimento:

```bash
# Subir PostgreSQL
docker-compose up -d

# Derrubar
docker-compose down
```

A tabela `outbox_tasks` é criada automaticamente pelo container usando o mesmo SQL embarcado no código.

## Configuração

A biblioteca usa o padrão **Functional Options** para configuração flexível:

### Opções do Service (New)

| Opção | Descrição | Default |
|-------|-----------|---------|
| `WithLoopInterval(d time.Duration)` | Intervalo entre execuções do retry loop | 1 minuto |

```go
// Exemplo: retry a cada 30 segundos
outbox := goutbox.New(store, goutbox.WithLoopInterval(30*time.Second))
```

### Opções por Evento (Do)

| Opção | Descrição | Default |
|-------|-----------|---------|
| `WithMaxAttempts(n int)` | Número máximo de tentativas antes de descartar | 3 |

```go
// Exemplo: até 5 tentativas para este evento
err := outbox.Do(ctx, "critical.event", payload, publisher, goutbox.WithMaxAttempts(5))
```

### Valores Default

Se valores inválidos (zero ou negativos) forem fornecidos, os defaults são usados automaticamente.

## Licença

Consulte o arquivo [LICENSE](LICENSE) para detalhes.