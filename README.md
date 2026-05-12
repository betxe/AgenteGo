# Script de métricas para RabbitMQ

Este proyecto contiene un script en Go llamado `SuscriberRabbitMQ.go` que recolecta métricas del sistema y las publica como mensaje JSON en una cola de RabbitMQ.

## Qué hace

El script realiza estos pasos:

1. Obtiene métricas del equipo.
2. Calcula el uso de CPU, RAM y disco.
3. Intenta leer la temperatura del sistema con varias fuentes.
4. Serializa todo en JSON.
5. Publica el resultado en RabbitMQ en la cola `it_metrics`.

## Requisitos

- Go instalado.
- Un servidor RabbitMQ accesible.
- Acceso de red al broker.

El archivo `go.mod` indica estas dependencias principales:

- `github.com/rabbitmq/amqp091-go`
- `github.com/shirou/gopsutil/v3`

## Estructura del mensaje

El mensaje enviado a RabbitMQ tiene esta forma:

```json
{
  "timestamp": 1710000000,
  "cpu_percent": 12.5,
  "ram_percent": 63.2,
  "disk_percent": 71.8,
  "temp_c": 45.3
}
```

### Campos

- `timestamp`: instante en Unix epoch.
- `cpu_percent`: porcentaje de uso de CPU.
- `ram_percent`: porcentaje de RAM usada.
- `disk_percent`: porcentaje de disco usado.
- `temp_c`: temperatura en grados Celsius. Puede ser `null` si no se pudo detectar.

## Cómo funciona la temperatura

La lectura de temperatura es mejor esfuerzo:

- Primero intenta obtener sensores con `gopsutil`.
- Si está en Windows, intenta consultas WMI mediante PowerShell.
- Si no encuentra un valor válido, deja `temp_c` en `null`.

Esto significa que la temperatura puede no estar disponible en todos los equipos, especialmente si el hardware o los permisos no permiten acceder al sensor.

## Configuración

El script acepta este parámetro:

- `-rabbitmq-url`: URL de conexión AMQP.

Valor por defecto:

```text
amqp://guest:guest@localhost:5672/
```

## Ejecución

Desde la carpeta del proyecto:

```bash
go run SuscriberRabbitMQ.go
```

Si necesitas indicar otra URL de RabbitMQ:

```bash
go run SuscriberRabbitMQ.go -rabbitmq-url="amqp://user:password@host:5672/"
```

También puedes compilarlo:

```bash
go build -o it-monitor.exe SuscriberRabbitMQ.go
```

Y ejecutar el binario generado:

```bash
.\it-monitor.exe -rabbitmq-url="amqp://user:password@host:5672/"
```

## Qué publica exactamente

El script:

- Abre una conexión a RabbitMQ.
- Declara la cola `it_metrics` si no existe.
- Publica el JSON en la cola usando el intercambio por defecto.
- Usa `application/json` como `Content-Type`.

## Consideraciones importantes

- El script toma una sola muestra por ejecución; no está pensado como proceso continuo.
- La lectura de disco usa el path raíz que resuelve la librería del sistema.
- Si RabbitMQ no está disponible, el script termina con error.
- Si la temperatura no puede obtenerse, el resto de métricas sigue enviándose normalmente.

## Ejemplo de uso esperado

1. Iniciar RabbitMQ.
2. Ejecutar el script.
3. Verificar que la cola `it_metrics` reciba el mensaje.

## Posibles mejoras

- Ejecutarlo en modo continuo con un intervalo fijo.
- Añadir logs más detallados.
- Hacer configurable el nombre de la cola.
- Añadir reintentos de conexión a RabbitMQ.
