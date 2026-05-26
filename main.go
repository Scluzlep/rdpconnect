package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

//go:embed index.html
var indexHTML []byte

type PortState struct {
	IP           string     `json:"ip"`
	Port         int        `json:"port"`
	Enabled      bool       `json:"enabled"`
	TCPAllowed   bool       `json:"tcpAllowed"`
	UDPAllowed   bool       `json:"udpAllowed"`
	TCPConnected bool       `json:"tcpConnected"`
	UDPConnected bool       `json:"udpConnected"`
	Connected    bool       `json:"connected"`
	OpenedAt     *time.Time `json:"openedAt"`
	LastSeenAt   *time.Time `json:"lastSeenAt"`
	IdleSince    *time.Time `json:"idleSince"`
	GraceUntil   *time.Time `json:"graceUntil"`
	MaxExpireAt  *time.Time `json:"maxExpireAt"`
}

type Settings struct {
	IPTablesChain         string `json:"iptablesChain"`
	MaxDurationProtection bool   `json:"maxDurationProtection"`
	MaxDurationMinutes    int    `json:"maxDurationMinutes"`
	IdleCloseMinutes      int    `json:"idleCloseMinutes"`
	InitialGraceMinutes   int    `json:"initialGraceMinutes"`
	ScanIntervalSeconds   int    `json:"scanIntervalSeconds"`
}

type StateFile struct {
	Rules    []PortState `json:"rules"`
	Settings Settings    `json:"settings"`
}

type APIData struct {
	IP            string               `json:"ip"`
	IPTablesChain string               `json:"iptablesChain"`
	Ports         map[string]PortState `json:"ports"`
	Settings      Settings             `json:"settings"`
}

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

var (
	appHost              string
	appPort              string
	stateFile            string
	iptablesChain        string
	maxDurationMinutes   int
	idleCloseMinutes     int
	initialGraceMinutes  int
	scanIntervalSeconds  int
	trustProxy           bool

	stateMutex sync.RWMutex
	appState   StateFile
)

func init() {
	// 尝试加载 .env 文件，如果不存在则忽略（开发/生产环境直接使用系统环境变量）
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("Error loading .env file: %v", err)
	}

	appHost              = getEnv("APP_HOST", "127.0.0.1")
	appPort              = getEnv("APP_PORT", "8080")
	stateFile            = getEnv("STATE_FILE", "./state.json")
	iptablesChain        = getEnv("IPTABLES_CHAIN", "RDP_JIFANG")
	maxDurationMinutes   = getEnvInt("MAX_DURATION_MINUTES", 180)
	idleCloseMinutes     = getEnvInt("IDLE_CLOSE_MINUTES", 3)
	initialGraceMinutes  = getEnvInt("INITIAL_GRACE_MINUTES", 10)
	scanIntervalSeconds  = getEnvInt("SCAN_INTERVAL_SECONDS", 10)
	trustProxy           = getEnvBool("TRUST_PROXY", true)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return fallback
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	loadState()
	initIPTables()

	go workerLoop()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/open1", func(w http.ResponseWriter, r *http.Request) { handleOpen(w, r, 33899) })
	http.HandleFunc("/api/close1", func(w http.ResponseWriter, r *http.Request) { handleClose(w, r, 33899) })
	http.HandleFunc("/api/open2", func(w http.ResponseWriter, r *http.Request) { handleOpen(w, r, 33889) })
	http.HandleFunc("/api/close2", func(w http.ResponseWriter, r *http.Request) { handleClose(w, r, 33889) })
	http.HandleFunc("/api/close-all", handleCloseAll)
	http.HandleFunc("/api/max-duration/enabled", func(w http.ResponseWriter, r *http.Request) { handleMaxDuration(w, r, true) })
	http.HandleFunc("/api/max-duration/disabled", func(w http.ResponseWriter, r *http.Request) { handleMaxDuration(w, r, false) })

	addr := net.JoinHostPort(appHost, appPort)
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// ---------------- IPTables ----------------

func initIPTables() {
	cmd := exec.Command("iptables", "-N", appState.Settings.IPTablesChain)
	if err := cmd.Run(); err != nil {
		log.Printf("Chain %s check (might already exist): %v", appState.Settings.IPTablesChain, err)
	}

	cmd = exec.Command("iptables", "-C", "INPUT", "-j", appState.Settings.IPTablesChain)
	if err := cmd.Run(); err != nil {
		cmd = exec.Command("iptables", "-I", "INPUT", "-j", appState.Settings.IPTablesChain)
		if err := cmd.Run(); err != nil {
			log.Printf("Failed to insert jump rule: %v", err)
		} else {
			log.Printf("Inserted jump rule for %s", appState.Settings.IPTablesChain)
		}
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()
	for i := range appState.Rules {
		rule := &appState.Rules[i]
		if rule.Enabled {
			if !checkRule(rule.IP, rule.Port, "tcp") { addRule(rule.IP, rule.Port, "tcp") }
			if !checkRule(rule.IP, rule.Port, "udp") { addRule(rule.IP, rule.Port, "udp") }
		}
	}
}

func checkRule(ip string, port int, proto string) bool {
	cmd := exec.Command("iptables", "-C", appState.Settings.IPTablesChain, "-p", proto, "-s", ip, "--dport", strconv.Itoa(port), "-j", "ACCEPT")
	return cmd.Run() == nil
}

func addRule(ip string, port int, proto string) {
	if checkRule(ip, port, proto) { return }
	cmd := exec.Command("iptables", "-I", appState.Settings.IPTablesChain, "-p", proto, "-s", ip, "--dport", strconv.Itoa(port), "-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to add rule %s %d %s: %v", ip, port, proto, err)
	} else {
		log.Printf("Added rule %s %d %s", ip, port, proto)
	}
}

func delRule(ip string, port int, proto string) {
	if !checkRule(ip, port, proto) { return }
	cmd := exec.Command("iptables", "-D", appState.Settings.IPTablesChain, "-p", proto, "-s", ip, "--dport", strconv.Itoa(port), "-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to del rule %s %d %s: %v", ip, port, proto, err)
	} else {
		log.Printf("Deleted rule %s %d %s", ip, port, proto)
	}
}

// ---------------- State Management ----------------

func loadState() {
	appState.Settings = Settings{
		IPTablesChain:         iptablesChain,
		MaxDurationProtection: true,
		MaxDurationMinutes:    maxDurationMinutes,
		IdleCloseMinutes:      idleCloseMinutes,
		InitialGraceMinutes:   initialGraceMinutes,
		ScanIntervalSeconds:   scanIntervalSeconds,
	}

	b, err := os.ReadFile(stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read state file: %v", err)
		}
		return
	}
	var s StateFile
	if err := json.Unmarshal(b, &s); err == nil {
		appState.Rules = s.Rules
		appState.Settings.MaxDurationProtection = s.Settings.MaxDurationProtection
	}
}

func saveState() {
	b, err := json.MarshalIndent(appState, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal state: %v", err)
		return
	}
	tmpFile := stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, b, 0644); err != nil {
		log.Printf("Failed to write temp state: %v", err)
		return
	}
	if err := os.Rename(tmpFile, stateFile); err != nil {
		log.Printf("Failed to rename state file: %v", err)
	}
}

func defaultPortState(ip string, port int) PortState {
	return PortState{
		IP:      ip,
		Port:    port,
		Enabled: false,
	}
}

// ---------------- Connection Tracking ----------------

func checkTCP(ip string, port int) bool {
	cmd := exec.Command("ss", "-tn", "dst", ip, "or", "src", ip)
	out, err := cmd.Output()
	if err != nil { return false }
	target := fmt.Sprintf(":%d", port)
	ipPrefix := ip + ":"
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 {
			if strings.HasPrefix(f[4], ipPrefix) && (strings.HasSuffix(f[3], target) || strings.HasSuffix(f[4], target)) {
				return true
			}
		}
	}
	return false
}

func checkUDP(ip string, port int) bool {
	cmd := exec.Command("conntrack", "-L", "-p", "udp")
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			hasSrc, hasPort := false, false
			for _, part := range f {
				if part == "src="+ip || part == "dst="+ip { hasSrc = true }
				if part == fmt.Sprintf("sport=%d", port) || part == fmt.Sprintf("dport=%d", port) { hasPort = true }
			}
			if hasSrc && hasPort { return true }
		}
		return false
	}
	// fallback to ss
	cmd = exec.Command("ss", "-un", "dst", ip, "or", "src", ip)
	out, err = cmd.Output()
	if err != nil { return false }
	target := fmt.Sprintf(":%d", port)
	ipPrefix := ip + ":"
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 {
			if strings.HasPrefix(f[4], ipPrefix) && (strings.HasSuffix(f[3], target) || strings.HasSuffix(f[4], target)) {
				return true
			}
		}
	}
	return false
}

func workerLoop() {
	ticker := time.NewTicker(time.Duration(appState.Settings.ScanIntervalSeconds) * time.Second)
	for range ticker.C {
		stateMutex.Lock()
		changed := false
		now := time.Now()
		var newRules []PortState

		for _, rule := range appState.Rules {
			if !rule.Enabled { continue }

			rule.TCPConnected = checkTCP(rule.IP, rule.Port)
			rule.UDPConnected = checkUDP(rule.IP, rule.Port)
			rule.Connected = rule.TCPConnected || rule.UDPConnected

			if rule.Connected {
				t := now
				rule.LastSeenAt = &t
				rule.IdleSince = nil
			} else if rule.GraceUntil != nil && now.After(*rule.GraceUntil) {
				if rule.IdleSince == nil {
					t := now
					rule.IdleSince = &t
				}
				idleLimit := time.Duration(appState.Settings.IdleCloseMinutes) * time.Minute
				if now.Sub(*rule.IdleSince) >= idleLimit {
					log.Printf("Auto closing idle port %d for %s", rule.Port, rule.IP)
					delRule(rule.IP, rule.Port, "tcp")
					delRule(rule.IP, rule.Port, "udp")
					changed = true
					continue
				}
			}

			if appState.Settings.MaxDurationProtection && rule.MaxExpireAt != nil && now.After(*rule.MaxExpireAt) {
				log.Printf("Auto closing expired port %d for %s", rule.Port, rule.IP)
				delRule(rule.IP, rule.Port, "tcp")
				delRule(rule.IP, rule.Port, "udp")
				changed = true
				continue
			}
			newRules = append(newRules, rule)
		}

		appState.Rules = newRules
		saveState()
		_ = changed // just indicating we saved the state including updated timestamps
		stateMutex.Unlock()
	}
}

// ---------------- HTTP Helpers ----------------

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeOk(w http.ResponseWriter, data interface{}) {
	writeJSON(w, APIResponse{Success: true, Message: "ok", Data: data})
}

func writeErr(w http.ResponseWriter, msg string) {
	writeJSON(w, APIResponse{Success: false, Message: msg})
}

func isIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

func getClientIP(r *http.Request) (string, error) {
	if trustProxy {
		if ip := r.Header.Get("CF-Connecting-IP"); ip != "" && isIPv4(ip) { return ip, nil }
		if ip := r.Header.Get("X-Real-IP"); ip != "" && isIPv4(ip) { return ip, nil }
		if ips := r.Header.Get("X-Forwarded-For"); ips != "" {
			parts := strings.Split(ips, ",")
			ip := strings.TrimSpace(parts[0])
			if isIPv4(ip) { return ip, nil }
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && isIPv4(host) { return host, nil }
	if isIPv4(r.RemoteAddr) { return r.RemoteAddr, nil }
	return "", fmt.Errorf("could not determine valid IPv4 address")
}

func writeStatusUnlocked(w http.ResponseWriter, ip string) {
	ports := map[string]PortState{
		"33899": defaultPortState(ip, 33899),
		"33889": defaultPortState(ip, 33889),
	}
	for _, rule := range appState.Rules {
		if rule.IP == ip {
			if rule.Port == 33899 { ports["33899"] = rule }
			if rule.Port == 33889 { ports["33889"] = rule }
		}
	}
	writeOk(w, APIData{
		IP:            ip,
		IPTablesChain: appState.Settings.IPTablesChain,
		Ports:         ports,
		Settings:      appState.Settings,
	})
}

// ---------------- API Handlers ----------------

func handleStatus(w http.ResponseWriter, r *http.Request) {
	ip, err := getClientIP(r)
	if err != nil {
		writeErr(w, err.Error())
		return
	}
	stateMutex.RLock()
	defer stateMutex.RUnlock()
	writeStatusUnlocked(w, ip)
}

func handleOpen(w http.ResponseWriter, r *http.Request, port int) {
	if r.Method != http.MethodPost {
		writeErr(w, "Method not allowed")
		return
	}
	ip, err := getClientIP(r)
	if err != nil {
		writeErr(w, err.Error())
		return
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()

	now := time.Now()
	graceUntil := now.Add(time.Duration(appState.Settings.InitialGraceMinutes) * time.Minute)
	var maxExpireAt *time.Time
	if appState.Settings.MaxDurationProtection {
		t := now.Add(time.Duration(appState.Settings.MaxDurationMinutes) * time.Minute)
		maxExpireAt = &t
	}

	found := false
	for i := range appState.Rules {
		rule := &appState.Rules[i]
		if rule.IP == ip && rule.Port == port {
			found = true
			rule.Enabled = true
			rule.TCPAllowed = true
			rule.UDPAllowed = true
			rule.OpenedAt = &now
			rule.GraceUntil = &graceUntil
			rule.MaxExpireAt = maxExpireAt
			rule.IdleSince = nil
			break
		}
	}
	if !found {
		appState.Rules = append(appState.Rules, PortState{
			IP: ip, Port: port, Enabled: true, TCPAllowed: true, UDPAllowed: true,
			OpenedAt: &now, GraceUntil: &graceUntil, MaxExpireAt: maxExpireAt,
		})
	}

	addRule(ip, port, "tcp")
	addRule(ip, port, "udp")
	saveState()
	writeStatusUnlocked(w, ip)
}

func handleClose(w http.ResponseWriter, r *http.Request, port int) {
	if r.Method != http.MethodPost {
		writeErr(w, "Method not allowed")
		return
	}
	ip, err := getClientIP(r)
	if err != nil {
		writeErr(w, err.Error())
		return
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()

	var newRules []PortState
	for _, rule := range appState.Rules {
		if rule.IP == ip && rule.Port == port {
			delRule(ip, rule.Port, "tcp")
			delRule(ip, rule.Port, "udp")
		} else {
			newRules = append(newRules, rule)
		}
	}
	appState.Rules = newRules
	saveState()
	writeStatusUnlocked(w, ip)
}

func handleCloseAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, "Method not allowed")
		return
	}
	ip, err := getClientIP(r)
	if err != nil {
		writeErr(w, err.Error())
		return
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()

	var newRules []PortState
	for _, rule := range appState.Rules {
		if rule.IP == ip {
			delRule(ip, rule.Port, "tcp")
			delRule(ip, rule.Port, "udp")
		} else {
			newRules = append(newRules, rule)
		}
	}
	appState.Rules = newRules
	saveState()
	writeStatusUnlocked(w, ip)
}

func handleMaxDuration(w http.ResponseWriter, r *http.Request, enabled bool) {
	if r.Method != http.MethodPost {
		writeErr(w, "Method not allowed")
		return
	}
	ip, err := getClientIP(r)
	if err != nil {
		writeErr(w, err.Error())
		return
	}
	stateMutex.Lock()
	defer stateMutex.Unlock()
	
	appState.Settings.MaxDurationProtection = enabled
	saveState()
	writeStatusUnlocked(w, ip)
}
