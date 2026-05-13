package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type Metrics struct {
	Timestamp   int64    `json:"timestamp"`
	CPUPercent  float64  `json:"cpu_percent"`
	RAMPercent  float64  `json:"ram_percent"`
	DiskPercent float64  `json:"disk_percent"`
	Temp        *float64 `json:"temp_c"`
}

// getTemperature intenta obtener la temperatura de CPU desde múltiples fuentes.
func getTemperature() *float64 {
	// Try gopsutil sensors
	t, _ := host.SensorsTemperatures()
	if len(t) > 0 {
		v := t[0].Temperature
		return &v
	}

	// En Windows, intentamos PowerShell + WMI
	if runtime.GOOS == "windows" {
		return getTemperatureViaPowerShell()
	}

	return nil
}

// getTemperatureViaPowerShell consulta WMI vía PowerShell para máxima compatibilidad.
func getTemperatureViaPowerShell() *float64 {
	// Query que busca MSAcpi_ThermalZoneTemperature (thermal zones del chipset).
	// Valor en décimas de Kelvin, convertir a Celsius.
	psCmd := `Get-WmiObject -Namespace "root\wmi" -Class MSAcpi_ThermalZoneTemperature -ErrorAction SilentlyContinue | Select-Object -First 1 | ForEach-Object { $_.CurrentTemperature }`

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		val, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
		if err == nil {
			// Convertir décimas de Kelvin a Celsius
			tempC := val/10.0 - 273.15
			if tempC > -50 && tempC < 150 { // Rango realista
				return &tempC
			}
		}
	}

	// Fallback: intenta Win32_SystemEnclosure
	psCmd = `Get-WmiObject Win32_SystemEnclosure -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty HotSwappable`
	cmd = exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	output, err = cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		val, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
		if err == nil && val > -50 && val < 150 {
			return &val
		}
	}

	// Último fallback: busca cualquier temperatura disponible en Win32_TemperatureProbe
	psCmd = `Get-WmiObject Win32_TemperatureProbe -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty CurrentReading`
	cmd = exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	output, err = cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		val, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
		if err == nil {
			// Si el valor es muy grande, asumimos que está en décimas
			if val > 1000 {
				val = val / 10.0
			}
			if val > -50 && val < 150 {
				return &val
			}
		}
	}

	return nil
}

func sendMetrics(rabbitURL string) {
	// 1. Recolección de datos
	c, err := cpu.Percent(time.Second, false)
	if err != nil {
		log.Printf("Error obteniendo CPU: %v", err)
		return
	}

	m, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("Error obteniendo RAM: %v", err)
		return
	}

	d, err := disk.Usage("/")
	if err != nil {
		log.Printf("Error obteniendo Disco: %v", err)
		return
	}

	// Nota: La temperatura depende mucho del OS y hardware
	currentTemp := getTemperature()

	payload := Metrics{
		Timestamp:   time.Now().Unix(),
		CPUPercent:  c[0],
		RAMPercent:  m.UsedPercent,
		DiskPercent: d.UsedPercent,
		Temp:        currentTemp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error al convertir a JSON: %v", err)
		return
	}

	// 2. Envío a RabbitMQ (se abre y cierra conexión para evitar "timeouts" por conexión inactiva en 5 min)
	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		log.Printf("Error conectando a RabbitMQ: %v", err)
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Printf("Error abriendo canal: %v", err)
		return
	}
	defer ch.Close()

	q, err := ch.QueueDeclare("it_metrics", false, false, false, false, nil)
	if err != nil {
		log.Printf("Error declarando cola: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = ch.PublishWithContext(ctx, "", q.Name, false, false, amqp091.Publishing{
		ContentType: "application/json",
		Body:        body,
	})

	if err != nil {
		log.Printf("Error al enviar mensaje a RabbitMQ: %v", err)
	} else {
		fmt.Printf("[%s] Datos enviados con éxito: %s\n", time.Now().Format("2006-01-02 15:04:05"), string(body))
	}
}

func main() {
	rabbitMQURL := flag.String("rabbitmq-url", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	flag.Parse()

	fmt.Println("Iniciando servicio de recolección métricas en segundo plano...")
	fmt.Println("Se enviarán métricas a RabbitMQ cada 5 minutos.")
	fmt.Println("Para detener el servicio, mata o finaliza el proceso desde el 'Administrador de Tareas'.")

	// Enviar primer lote de métricas de inmediato
	sendMetrics(*rabbitMQURL)

	// Crear ticker para intervalos de 5 minutos
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Ciclo 'infinito' bloqueante (el proceso principal nunca terminará a menos que se mate)
	for range ticker.C {
		sendMetrics(*rabbitMQURL)
	}
}
