package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/hamba/avro/v2"
	"github.com/rabbitmq/amqp091-go"
)

func runConsumer() {
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
