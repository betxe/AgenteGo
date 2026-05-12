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

func main() {
	rabbitMQURL := flag.String("rabbitmq-url", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	flag.Parse()

	// 1. Recolección de datos
	c, _ := cpu.Percent(time.Second, false)
	m, _ := mem.VirtualMemory()
	d, _ := disk.Usage("/")
	// Nota: La temperatura depende mucho del OS y hardware
	// En muchos casos requiere permisos de admin. Intentamos sensores
	// y, como fallback en Windows, consultamos WMI.
	currentTemp := getTemperature()

	payload := Metrics{
		Timestamp:   time.Now().Unix(),
		CPUPercent:  c[0],
		RAMPercent:  m.UsedPercent,
		DiskPercent: d.UsedPercent,
		Temp:        currentTemp,
	}

	body, _ := json.Marshal(payload)

	// 2. Envío a RabbitMQ
	conn, err := amqp091.Dial(*rabbitMQURL)
	if err != nil {
		log.Fatalf("Error conectando a Rabbit: %v", err)
	}
	defer conn.Close()

	ch, _ := conn.Channel()
	defer ch.Close()

	q, _ := ch.QueueDeclare("it_metrics", false, false, false, false, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = ch.PublishWithContext(ctx, "", q.Name, false, false, amqp091.Publishing{
		ContentType: "application/json",
		Body:        body,
	})

	if err != nil {
		fmt.Println("Error al enviar:", err)
	} else {
		fmt.Printf("Datos enviados con éxito: %s\n", string(body))
	}
}
