package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/hamba/avro/v2"
	"github.com/rabbitmq/amqp091-go"
)

type Metrics struct {
	Timestamp   int64    `avro:"timestamp"`
	CPUPercent  float64  `avro:"cpu_percent"`
	RAMPercent  float64  `avro:"ram_percent"`
	DiskPercent float64  `avro:"disk_percent"`
	Temp        *float64 `avro:"temp_c"`
}

var metricsSchema avro.Schema

func init() {
	schemaStr := `{
		"type": "record",
		"name": "Metrics",
		"namespace": "com.example",
		"fields": [
			{"name": "timestamp", "type": "long"},
			{"name": "cpu_percent", "type": "double"},
			{"name": "ram_percent", "type": "double"},
			{"name": "disk_percent", "type": "double"},
			{"name": "temp_c", "type": ["null", "double"], "default": null}
		]
	}`
	var err error
	metricsSchema, err = avro.Parse(schemaStr)
	if err != nil {
		log.Fatalf("Error parsing avro schema: %v", err)
	}
}

func main() {
	rabbitMQURL := flag.String("rabbitmq-url", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	flag.Parse()

	conn, err := amqp091.Dial(*rabbitMQURL)
	if err != nil {
		log.Fatalf("Error conectando a RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Error abriendo canal: %v", err)
	}
	defer ch.Close()

	// Declaramos la cola igual que en el publisher
	q, err := ch.QueueDeclare("it_metrics", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Error declarando cola: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatalf("Error registrando el consumidor: %v", err)
	}

	var forever chan struct{}

	go func() {
		for d := range msgs {
			var payload Metrics
			err := avro.Unmarshal(metricsSchema, d.Body, &payload)
			if err != nil {
				log.Printf("Error deserializando Avro: %v", err)
				continue
			}
			fmt.Printf("✅ Mensaje recibido en RabbitMQ y decodificado correctamente:\n%+v\n\n", payload)
		}
	}()

	log.Printf(" [*] Escuchando mensajes en la cola '%s'. Para salir pulsa CTRL+C", q.Name)
	<-forever // Mantiene el programa corriendo de forma indefinida
}
