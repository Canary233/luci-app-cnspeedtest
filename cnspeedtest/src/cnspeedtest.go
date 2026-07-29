package main

import (
	"bufio"
	"compress/gzip"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	appPkg                = "com.cnspeedtest.globalspeed"
	defaultHost           = "111.8.9.157"
	defaultPort           = 65499
	boundary              = "00content0boundary00"
	bandwidth             = 200
	downloadThreads       = 8
	uploadThreads         = 4
	testDuration          = 8 * time.Second
	sampleInterval        = 500 * time.Millisecond
	stableWarmupMs        = 1000
	stableCooldownMs      = 500
	displayEmaAlpha       = 0.22
	transferSocketTimeout = 450 * time.Millisecond
	tcpConnectTimeout     = 1500 * time.Millisecond
	httpTimeout           = 3 * time.Second
	downloadProbeTimeout  = 2500 * time.Millisecond
	releaseKeyTimeout     = 800 * time.Millisecond
	uploadDrainTimeout    = 650 * time.Millisecond
	autoNodeLimit         = 5
	dalvikUA              = "Dalvik/2.1.0 (Linux; U; Android 13; CnSpeedtest Build/Android)"
	downloadUA            = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Safari/537.36"
	uploadUA              = "Dalvik/1.6.0 (Linux; U; Android 4.2.2; GT-I9505 Build/JDQ39)"
)

type ipLocation struct {
	PublicIP string `json:"public_ip"`
	Province string `json:"province"`
	City     string `json:"city"`
	Carrier  string `json:"carrier"`
}

func (l ipLocation) displayLocation() string {
	if l.City != "" {
		return l.City
	}
	if l.Province != "" {
		return l.Province
	}
	return "Guangzhou"
}

type node struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Name      string `json:"name"`
	HostID    string `json:"host_id"`
	LatencyMs *int   `json:"latency_ms"`
}

func (n node) displayName() string {
	if n.Name != "" {
		return n.Name
	}
	if n.HostID != "" {
		return n.HostID
	}
	return fmt.Sprintf("%s:%d", n.Host, n.Port)
}

type dovalidResult struct {
	Key       string
	Timestamp int64
	Mode      string
}

type transferResult struct {
	AverageMbps float64       `json:"average_mbps"`
	PeakMbps    float64       `json:"peak_mbps"`
	Bytes       int64         `json:"bytes"`
	Errors      int64         `json:"errors"`
	Samples     []speedSample `json:"samples,omitempty"`
}

type speedSample struct {
	ElapsedMs int64   `json:"elapsed_ms"`
	Mbps      float64 `json:"mbps"`
}

type finalResult struct {
	DownloadMbps    float64       `json:"download_mbps"`
	UploadMbps      float64       `json:"upload_mbps"`
	PingMs          int           `json:"ping_ms"`
	JitterMs        int           `json:"jitter_ms"`
	ServerName      string        `json:"server_name"`
	PublicIP        string        `json:"public_ip"`
	Location        string        `json:"location"`
	Carrier         string        `json:"carrier"`
	Node            node          `json:"node"`
	DownloadSamples []speedSample `json:"download_samples,omitempty"`
	UploadSamples   []speedSample `json:"upload_samples,omitempty"`
}

type options struct {
	listNodes      bool
	measureLatency bool
	host           string
	port           int
	name           string
	iface          string
	duration       time.Duration
	downThreads    int
	upThreads      int
	noDownload     bool
	noUpload       bool
	jsonOut        bool
	progressJSON   bool
	pause          bool
	quiet          bool
	verbose        bool
}

var bindIface string

func main() {
	var seconds float64
	opt := options{}
	rawArgCount := len(os.Args) - 1
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "用法: %s [选项]\n\n选项:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.BoolVar(&opt.listNodes, "list-nodes", false, "列出测速节点后退出")
	flag.BoolVar(&opt.measureLatency, "measure-latency", false, "列节点时测量 TCP 延迟")
	flag.StringVar(&opt.host, "host", "", "手动指定测速服务器地址或 IP")
	flag.IntVar(&opt.port, "port", defaultPort, "手动指定服务器端口")
	flag.StringVar(&opt.name, "name", "", "手动指定服务器显示名称")
	flag.StringVar(&opt.iface, "I", "", "指定出口网络接口（Linux/OpenWrt）")
	flag.Float64Var(&seconds, "duration", testDuration.Seconds(), "每个测速阶段的秒数")
	flag.IntVar(&opt.downThreads, "download-threads", downloadThreads, "下载线程数")
	flag.IntVar(&opt.upThreads, "upload-threads", uploadThreads, "上传线程数")
	flag.BoolVar(&opt.noDownload, "no-download", false, "跳过下载测速")
	flag.BoolVar(&opt.noUpload, "no-upload", false, "跳过上传测速")
	flag.BoolVar(&opt.jsonOut, "json", false, "以 JSON 输出最终结果")
	flag.BoolVar(&opt.progressJSON, "progress-json", false, "以 NDJSON 输出测速进度和最终结果")
	flag.BoolVar(&opt.pause, "pause", false, "测速完成后等待按 Enter 退出")
	flag.BoolVar(&opt.quiet, "q", false, "只输出最终摘要")
	flag.BoolVar(&opt.quiet, "quiet", false, "只输出最终摘要")
	flag.BoolVar(&opt.verbose, "v", false, "输出诊断信息到 stderr")
	flag.BoolVar(&opt.verbose, "verbose", false, "输出诊断信息到 stderr")
	flag.Parse()
	if rawArgCount == 0 && defaultPauseAfterCompletion() {
		opt.pause = true
	}

	if seconds <= 0 || opt.downThreads < 1 || opt.upThreads < 1 {
		fmt.Fprintln(os.Stderr, "invalid duration or thread count")
		os.Exit(2)
	}
	opt.duration = time.Duration(seconds * float64(time.Second))
	if opt.iface != "" {
		if !bindIfaceSupported() {
			fmt.Fprintln(os.Stderr, "当前平台不支持 -I 接口绑定")
			os.Exit(2)
		}
		if _, err := net.InterfaceByName(opt.iface); err != nil {
			fmt.Fprintf(os.Stderr, "无效接口 %q: %v\n", opt.iface, err)
			os.Exit(2)
		}
		bindIface = opt.iface
	}

	var err error
	if opt.listNodes {
		err = listNodes(opt)
	} else {
		var result finalResult
		result, err = runSpeedtest(opt)
		if err == nil {
			if opt.progressJSON {
				printProgressEvent(map[string]any{"type": "result", "result": result})
			} else {
				printResult(result, opt)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		pauseBeforeExit(opt)
		os.Exit(1)
	}
	pauseBeforeExit(opt)
}

func printResult(result finalResult, opt options) {
	if opt.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		return
	}
	fmt.Printf("结果: 下载 %.2f Mbps, 上传 %.2f Mbps, Ping %d ms, Jitter %d ms\n",
		result.DownloadMbps, result.UploadMbps, result.PingMs, result.JitterMs)
	fmt.Printf("节点: %s (%s:%d)\n", result.ServerName, result.Node.Host, result.Node.Port)
	fmt.Printf("网络: %s %s %s\n", result.PublicIP, result.Location, result.Carrier)
}

func defaultPauseAfterCompletion() bool {
	return runtime.GOOS == "windows"
}

func pauseBeforeExit(opt options) {
	if !opt.pause || opt.jsonOut || opt.progressJSON {
		return
	}
	fmt.Print("\n按 Enter 键退出...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func runSpeedtest(opt options) (finalResult, error) {
	clientID := newClientID()

	loc, err := getIpLocation()
	if err != nil {
		if opt.verbose {
			fmt.Fprintln(os.Stderr, "location lookup failed:", err)
		}
		loc = ipLocation{PublicIP: "113.89.234.12", City: "Guangzhou", Carrier: "China Mobile"}
	}

	var nodes []node
	if opt.host == "" {
		nodes, err = discoverNodes(loc)
		if err != nil && opt.verbose {
			fmt.Fprintln(os.Stderr, "node discovery failed:", err)
		}
	}
	var manual *node
	if opt.host != "" {
		name := opt.name
		if name == "" {
			name = "manual"
		}
		n := node{Host: opt.host, Port: opt.port, Name: name}
		manual = &n
	}

	n, auth, latencies, err := prepareNode(nodes, clientID, manual, opt.verbose)
	if err != nil {
		return finalResult{}, err
	}
	ping := 0
	valid := make([]float64, 0, len(latencies))
	for _, latency := range latencies {
		if latency >= 0 {
			valid = append(valid, latency)
		}
	}
	if len(valid) > 0 {
		sort.Float64s(valid)
		ping = int(math.Round(valid[0]))
	}
	jitter := int(math.Round(calculateJitter(latencies)))
	n.LatencyMs = &ping

	progress := func(phase string, speed float64, elapsedMs int64) {
		if opt.progressJSON {
			printProgressEvent(map[string]any{
				"type":       "sample",
				"phase":      phase,
				"elapsed_ms": elapsedMs,
				"mbps":       speed,
			})
			return
		}
		if opt.quiet || opt.jsonOut {
			return
		}
		label := "下载"
		if phase == "upload" {
			label = "上传"
		}
		fmt.Printf("\r%s: %8.2f Mbps", label, speed)
	}

	if !opt.quiet && !opt.jsonOut {
		fmt.Printf("节点: %s (%s:%d), ping %d ms, jitter %d ms\n", n.displayName(), n.Host, n.Port, ping, jitter)
	}

	download := transferResult{}
	upload := transferResult{}
	defer releaseKey(n, auth.Key)
	if !opt.noDownload {
		download = measureDownload(n, auth.Key, auth.Timestamp, opt.duration, opt.downThreads, progress)
		if !opt.quiet && !opt.jsonOut {
			fmt.Printf("\r下载: %8.2f Mbps\n", download.AverageMbps)
		}
	}
	if !opt.noUpload {
		upload = measureUpload(n, auth.Key, opt.duration, opt.upThreads, progress)
		if !opt.quiet && !opt.jsonOut {
			fmt.Printf("\r上传: %8.2f Mbps\n", upload.AverageMbps)
		}
	}

	return finalResult{
		DownloadMbps:    download.AverageMbps,
		UploadMbps:      upload.AverageMbps,
		PingMs:          ping,
		JitterMs:        jitter,
		ServerName:      n.displayName(),
		PublicIP:        loc.PublicIP,
		Location:        loc.displayLocation(),
		Carrier:         loc.Carrier,
		Node:            n,
		DownloadSamples: download.Samples,
		UploadSamples:   upload.Samples,
	}, nil
}

func listNodes(opt options) error {
	loc, err := getIpLocation()
	if err != nil {
		return err
	}
	nodes, err := discoverNodes(loc)
	if err != nil {
		return err
	}
	for i := range nodes {
		if opt.measureLatency {
			lat := tcpLatency(nodes[i])
			if lat >= 0 {
				ms := int(math.Round(lat))
				nodes[i].LatencyMs = &ms
			}
		}
	}
	if opt.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(nodes)
	}
	for _, n := range nodes {
		lat := "-"
		if n.LatencyMs != nil {
			lat = fmt.Sprintf("%d ms", *n.LatencyMs)
		}
		fmt.Printf("%s:%d\t%s\t%s\t%s\n", n.Host, n.Port, lat, n.displayName(), n.HostID)
	}
	return nil
}

func newClientID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	seed := int64(0)
	for _, x := range b {
		seed = seed*257 + int64(x)
	}
	r := mathrand.New(mathrand.NewSource(seed))
	value := "35"
	for i := 0; i < 13; i++ {
		value += strconv.Itoa(r.Intn(10))
	}
	return value
}

func boundDialer(timeout time.Duration) net.Dialer {
	dialer := net.Dialer{Timeout: timeout}
	configureBindIface(&dialer)
	return dialer
}

func dialTCP(address string, timeout time.Duration) (net.Conn, error) {
	dialer := boundDialer(timeout)
	return dialer.Dial("tcp", address)
}

func newHTTPClient(timeout time.Duration) *http.Client {
	dialer := boundDialer(timeout)
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dialer.DialContext,
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: timeout,
		},
	}
}

func httpGet(rawURL string, headers map[string]string, timeout time.Duration) (string, error) {
	client := newHTTPClient(timeout)
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		body = gz
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(data), fmt.Errorf("http %d", resp.StatusCode)
	}
	return string(data), nil
}

func getServerTime() int64 {
	text, err := httpGet(
		"http://dlc.duoweisoft.com:8096/dataServer/time.php",
		map[string]string{"User-Agent": "Mozilla/5.0 (Windows NT 6.1; Trident/7.0; rv:11.0) like Gecko"},
		httpTimeout,
	)
	if err != nil {
		return time.Now().Unix()
	}
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return time.Now().Unix()
	}
	return value
}

func getIpLocation() (ipLocation, error) {
	text, err := httpGet(
		"https://dlcv2.cnspeedtest.cn:8443/dataServer/getIpLocSP.php",
		map[string]string{"User-Agent": dalvikUA},
		httpTimeout,
	)
	if err != nil {
		return ipLocation{}, err
	}
	parts := strings.Split(strings.TrimSpace(text), "|")
	loc := ipLocation{Carrier: "China Mobile"}
	if len(parts) > 0 {
		loc.PublicIP = parts[0]
	}
	var arr []any
	if len(parts) > 1 && strings.HasPrefix(parts[1], "[") {
		_ = json.Unmarshal([]byte(parts[1]), &arr)
	}
	if len(arr) > 1 {
		loc.Province = normalizeProvince(fmt.Sprint(arr[1]))
	}
	if len(arr) > 2 {
		loc.City = normalizeCity(fmt.Sprint(arr[2]))
	}
	if len(parts) > 3 {
		loc.Carrier = parts[3]
	}
	if loc.Carrier == "" && len(arr) > 4 {
		loc.Carrier = fmt.Sprint(arr[4])
	}
	if loc.Carrier == "" {
		loc.Carrier = "China Mobile"
	}
	return loc, nil
}

type carrierProfile struct {
	Carrier      string
	MobileOperID string
}

func discoverNodes(loc ipLocation) ([]node, error) {
	profiles := carrierProfiles(loc.Carrier)
	seenProfiles := map[string]bool{}
	seenNodes := map[string]bool{}
	nodes := []node{}
	var firstErr error

	for _, profile := range profiles {
		key := strings.ToLower(profile.Carrier) + "|" + profile.MobileOperID
		if profile.Carrier == "" || seenProfiles[key] {
			continue
		}
		seenProfiles[key] = true
		items, err := discoverNodesFor(loc, profile)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, item := range items {
			nodeKey := item.Host + ":" + strconv.Itoa(item.Port)
			if item.HostID != "" {
				nodeKey = item.HostID + "|" + nodeKey
			}
			if seenNodes[nodeKey] {
				continue
			}
			seenNodes[nodeKey] = true
			nodes = append(nodes, item)
		}
	}
	if len(nodes) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return nodes, nil
}

func carrierProfiles(current string) []carrierProfile {
	current = strings.TrimSpace(current)
	currentID := carrierMobileOperID(current)
	profiles := []carrierProfile{}
	if current != "" {
		profiles = append(profiles, carrierProfile{Carrier: current, MobileOperID: currentID})
		if currentID != "46001" {
			profiles = append(profiles, carrierProfile{Carrier: current, MobileOperID: "46001"})
		}
	}
	profiles = append(profiles,
		carrierProfile{Carrier: "电信", MobileOperID: "46003"},
		carrierProfile{Carrier: "中国电信", MobileOperID: "46003"},
		carrierProfile{Carrier: "China Telecom", MobileOperID: "46003"},
		carrierProfile{Carrier: "联通", MobileOperID: "46001"},
		carrierProfile{Carrier: "中国联通", MobileOperID: "46001"},
		carrierProfile{Carrier: "China Unicom", MobileOperID: "46001"},
		carrierProfile{Carrier: "移动", MobileOperID: "46000"},
		carrierProfile{Carrier: "中国移动", MobileOperID: "46000"},
		carrierProfile{Carrier: "China Mobile", MobileOperID: "46000"},
		carrierProfile{Carrier: "广电", MobileOperID: "46015"},
		carrierProfile{Carrier: "中国广电", MobileOperID: "46015"},
		carrierProfile{Carrier: "China Broadcast Network", MobileOperID: "46015"},
	)
	return profiles
}

func carrierMobileOperID(carrier string) string {
	value := strings.ToLower(strings.TrimSpace(carrier))
	switch {
	case strings.Contains(value, "电信") || strings.Contains(value, "telecom") || strings.Contains(value, "ctcc"):
		return "46003"
	case strings.Contains(value, "移动") || strings.Contains(value, "mobile") || strings.Contains(value, "cmcc"):
		return "46000"
	case strings.Contains(value, "广电") || strings.Contains(value, "broadcast") || strings.Contains(value, "cbn"):
		return "46015"
	default:
		return "46001"
	}
}

func discoverNodesFor(loc ipLocation, profile carrierProfile) ([]node, error) {
	params := url.Values{}
	params.Set("ip", loc.PublicIP)
	params.Set("network", "4")
	params.Set("province", loc.Province)
	params.Set("city", loc.City)
	params.Set("wifioper", profile.Carrier)
	params.Set("mobileoperid", profile.MobileOperID)
	params.Set("ipv6", "0")
	params.Set("model", "Android")
	params.Set("pkg", appPkg)
	text, err := httpGet(
		"http://dlc.duoweisoft.com:8096/dataServer/mobilematch_many.php?"+params.Encode(),
		map[string]string{"User-Agent": dalvikUA},
		httpTimeout,
	)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	nodes := make([]node, 0, len(raw))
	for _, item := range raw {
		host := strings.TrimSpace(fmt.Sprint(item["hostip"]))
		if host == "" || host == "<nil>" {
			continue
		}
		port := defaultPort
		if p, ok := item["port"].(float64); ok && p > 0 {
			port = int(p)
		}
		nodes = append(nodes, node{
			Host:   host,
			Port:   port,
			Name:   valueString(item["hostname"]),
			HostID: valueString(item["hostid"]),
		})
	}
	return nodes, nil
}

func valueString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func prepareNode(nodes []node, clientID string, manual *node, verbose bool) (node, dovalidResult, []float64, error) {
	candidates := []node{}
	if manual != nil {
		candidates = append(candidates, *manual)
	} else {
		limit := autoNodeLimit
		if len(nodes) < limit {
			limit = len(nodes)
		}
		candidates = append(candidates, nodes[:limit]...)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, node{Host: defaultHost, Port: defaultPort, Name: "default"})
	}

	type scoredNode struct {
		lat float64
		n   node
	}
	scored := []scoredNode{}
	for _, n := range candidates {
		latency := tcpLatency(n)
		if verbose {
			fmt.Fprintf(os.Stderr, "candidate %s %s:%d latency=%.0fms\n", n.displayName(), n.Host, n.Port, latency)
		}
		if latency >= 0 || manual != nil {
			ms := int(math.Round(latency))
			if latency >= 0 {
				n.LatencyMs = &ms
			}
			scored = append(scored, scoredNode{lat: latency, n: n})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		a, b := scored[i].lat, scored[j].lat
		if a < 0 {
			a = math.MaxFloat64
		}
		if b < 0 {
			b = math.MaxFloat64
		}
		return a < b
	})
	for _, item := range scored {
		auth := tryDovalidWithRetries(item.n, clientID)
		if auth.Key != "" && probeDownload(item.n, auth.Key, auth.Timestamp) {
			return item.n, auth, tcpLatencies(item.n, 4), nil
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "skip %s: dovalid/probe failed\n", item.n.displayName())
		}
	}
	return node{}, dovalidResult{}, nil, errors.New("failed to authorize a speed test node")
}

func tryDovalidWithRetries(n node, clientID string) dovalidResult {
	auth := tryDovalid(n, clientID, getServerTime())
	if auth.Key != "" {
		return auth
	}
	time.Sleep(250 * time.Millisecond)
	return dovalidResult{}
}

func tryDovalid(n node, clientID string, timestamp int64) dovalidResult {
	base := url.Values{}
	base.Set("key", "")
	base.Set("flag", "true")
	base.Set("bandwidth", strconv.Itoa(bandwidth))
	base.Set("model", "Android")
	base.Set("imei", clientID)
	base.Set("time", strconv.FormatInt(timestamp, 10))
	legacy := md5Hex("model=Android&imei=" + clientID + "&stime=" + strconv.FormatInt(timestamp, 10))
	attempts := []struct {
		mode  string
		extra map[string]string
	}{
		{"globalspeed-4.4-native", map[string]string{"app": "globalspeed", "token": globalspeedToken(clientID, timestamp), "pkg": appPkg}},
		{"legacy-2024-no-app-pkg", map[string]string{"token": legacy}},
		{"legacy-2024-with-app-pkg", map[string]string{"app": "globalspeed", "token": legacy, "pkg": appPkg}},
		{"no-token-debug-only", map[string]string{"app": "globalspeed", "pkg": appPkg}},
	}
	for _, attempt := range attempts {
		params := url.Values{}
		for k, v := range base {
			params[k] = v
		}
		for k, v := range attempt.extra {
			params.Set(k, v)
		}
		text, err := httpGet(
			fmt.Sprintf("http://%s:%d/speed/dovalid?%s", n.Host, n.Port, params.Encode()),
			map[string]string{"User-Agent": dalvikUA},
			httpTimeout,
		)
		if err == nil {
			if key := parseDovalidKey(text); key != "" {
				return dovalidResult{Key: key, Timestamp: timestamp, Mode: attempt.mode}
			}
		}
	}
	return dovalidResult{Timestamp: timestamp}
}

func md5Hex(input string) string {
	sum := md5.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func globalspeedToken(clientID string, timestamp int64) string {
	return md5Hex(md5Hex("model=Android&imei="+clientID) + md5Hex(fmt.Sprintf("stime=%d&band=%d&rand=12345555", timestamp, bandwidth)))
}

func parseDovalidKey(text string) string {
	text = strings.TrimSpace(text)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^1-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`),
		regexp.MustCompile(`^1-([0-9a-fA-F-]{16,64})`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(text); match != nil {
			return match[1]
		}
	}
	return ""
}

func tcpLatency(n node) float64 {
	start := time.Now()
	conn, err := dialTCP(fmt.Sprintf("%s:%d", n.Host, n.Port), tcpConnectTimeout)
	if err != nil {
		return -1
	}
	_ = conn.Close()
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func tcpLatencies(n node, count int) []float64 {
	values := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, tcpLatency(n))
		time.Sleep(80 * time.Millisecond)
	}
	return values
}

func calculateJitter(latencies []float64) float64 {
	valid := []float64{}
	for _, latency := range latencies {
		if latency >= 0 {
			valid = append(valid, latency)
		}
	}
	if len(valid) < 2 {
		return 0
	}
	sum := 0.0
	for _, x := range valid {
		sum += x
	}
	avg := sum / float64(len(valid))
	var variance float64
	for _, x := range valid {
		d := x - avg
		variance += d * d
	}
	return math.Sqrt(variance / float64(len(valid)))
}

func rawDownloadRequest(n node, key string, timestamp int64) []byte {
	path := fmt.Sprintf("/speed/File(1G).dl?r=%d&key=%s", timestamp, url.QueryEscape(key))
	return []byte(fmt.Sprintf("GET %s HTTP/1.1\r\nAccept: */*\r\nConnection: close\r\nUser-Agent: %s\r\nHost: %s:%d\r\n\r\n", path, downloadUA, n.Host, n.Port))
}

func readHTTPStatus(conn net.Conn) int {
	buf := make([]byte, 0, 4096)
	tmp := []byte{0}
	last4 := uint32(0)
	for len(buf) < 65536 {
		n, err := conn.Read(tmp)
		if err != nil || n <= 0 {
			break
		}
		b := tmp[0]
		buf = append(buf, b)
		last4 = (last4 << 8) | uint32(b)
		if last4 == 0x0d0a0d0a {
			break
		}
	}
	first := ""
	if len(buf) > 0 {
		first = strings.SplitN(string(buf), "\n", 2)[0]
	}
	match := regexp.MustCompile(`HTTP/\d(?:\.\d)?\s+(\d{3})`).FindStringSubmatch(first)
	if match == nil {
		return 0
	}
	status, _ := strconv.Atoi(match[1])
	return status
}

func probeDownload(n node, key string, timestamp int64) bool {
	conn, err := dialTCP(fmt.Sprintf("%s:%d", n.Host, n.Port), downloadProbeTimeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(downloadProbeTimeout))
	if _, err := conn.Write(rawDownloadRequest(n, key, timestamp)); err != nil {
		return false
	}
	status := readHTTPStatus(conn)
	return status >= 200 && status < 300
}

func releaseKey(n node, key string) bool {
	if key == "" {
		return false
	}
	client := newHTTPClient(releaseKeyTimeout)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%d/speed/dovalid?key=%s", n.Host, n.Port, url.QueryEscape(key)), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Charset", "utf-8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", dalvikUA)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode == 200 && strings.HasPrefix(strings.TrimSpace(string(data)), "1-")
}

func printProgressEvent(value any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func measureDownload(n node, key string, timestamp int64, duration time.Duration, threads int, progress func(string, float64, int64)) transferResult {
	var fatal atomic.Bool
	return measureTransfer("download", duration, progress, func(end time.Time, total *atomic.Int64, errors *atomic.Int64, cancel <-chan struct{}) []threadJoin {
		joins := make([]threadJoin, 0, threads)
		for i := 0; i < threads; i++ {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				downloadWorker(n, key, timestamp, end, total, errors, &fatal, cancel)
			}()
			joins = append(joins, wg.Wait)
		}
		return joins
	})
}

type threadJoin func()

func downloadWorker(n node, key string, timestamp int64, end time.Time, total *atomic.Int64, errors *atomic.Int64, fatal *atomic.Bool, cancel <-chan struct{}) {
	request := rawDownloadRequest(n, key, timestamp)
	buf := make([]byte, 262144)
	for time.Now().Before(end) && !fatal.Load() && !isCanceled(cancel) {
		conn, err := dialTCP(fmt.Sprintf("%s:%d", n.Host, n.Port), tcpConnectTimeout)
		if err != nil {
			errors.Add(1)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(transferSocketTimeout))
		_, err = conn.Write(request)
		if err != nil {
			_ = conn.Close()
			errors.Add(1)
			continue
		}
		status := readHTTPStatus(conn)
		if status == 0 || status >= 400 {
			_ = conn.Close()
			errors.Add(1)
			if status == 0 || status < 500 {
				fatal.Store(true)
			}
			return
		}
		for time.Now().Before(end) && !fatal.Load() && !isCanceled(cancel) {
			_ = conn.SetReadDeadline(time.Now().Add(transferSocketTimeout))
			nread, err := conn.Read(buf)
			if nread > 0 {
				total.Add(int64(nread))
			}
			if err != nil {
				break
			}
		}
		_ = conn.Close()
	}
}

func measureUpload(n node, key string, duration time.Duration, threads int, progress func(string, float64, int64)) transferResult {
	chunk := make([]byte, 262144)
	_, _ = rand.Read(chunk)
	var sockets sync.Map
	result := measureTransfer("upload", duration, progress, func(end time.Time, total *atomic.Int64, errors *atomic.Int64, cancel <-chan struct{}) []threadJoin {
		joins := make([]threadJoin, 0, threads)
		for i := 0; i < threads; i++ {
			var wg sync.WaitGroup
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				uploadWorker(n, key, end, total, errors, chunk, cancel, &sockets)
			}(i)
			joins = append(joins, wg.Wait)
		}
		return joins
	})
	sockets.Range(func(k, _ any) bool {
		if conn, ok := k.(net.Conn); ok {
			_ = conn.Close()
		}
		return true
	})
	return result
}

func uploadWorker(n node, key string, end time.Time, total *atomic.Int64, errors *atomic.Int64, chunk []byte, cancel <-chan struct{}, sockets *sync.Map) {
	stamp := time.Now().Format("20060102_150405_000")
	formHead := fmt.Sprintf("--%s\r\nContent-Disposition: form-data; name=\"upload\";filename=\"SPEED_%s\"\r\n\r\n", boundary, stamp)
	request := []byte(fmt.Sprintf(
		"POST /speed/doAnalsLoad.do HTTP/1.1\r\nConnection: close\r\nCache-Control: no-cache\r\nCharset: UTF-8\r\nKey: %s\r\nContent-Type: multipart/form-data;boundary=%s\r\nUser-Agent: %s\r\nHost: %s:%d\r\nAccept-Encoding: gzip\r\nContent-Length: 900000000\r\n\r\n%s",
		key, boundary, uploadUA, n.Host, n.Port, formHead,
	))
	conn, err := dialTCP(fmt.Sprintf("%s:%d", n.Host, n.Port), httpTimeout)
	if err != nil {
		if time.Now().Add(50 * time.Millisecond).Before(end) {
			errors.Add(1)
		}
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetWriteBuffer(262144)
	}
	sockets.Store(conn, true)
	defer func() {
		sockets.Delete(conn)
		_ = conn.Close()
	}()
	_ = conn.SetWriteDeadline(end.Add(uploadDrainTimeout))
	writer := bufio.NewWriterSize(conn, len(chunk))
	if _, err := writer.Write(request); err != nil {
		errors.Add(1)
		return
	}
	if err := writer.Flush(); err != nil {
		errors.Add(1)
		return
	}
	for time.Now().Before(end) && !isCanceled(cancel) {
		nwritten, err := writer.Write(chunk)
		if nwritten > 0 {
			total.Add(int64(nwritten))
		}
		if err != nil {
			break
		}
		if err := writer.Flush(); err != nil {
			break
		}
	}
}

func measureTransfer(phase string, duration time.Duration, progress func(string, float64, int64), startWorkers func(time.Time, *atomic.Int64, *atomic.Int64, <-chan struct{}) []threadJoin) transferResult {
	var total atomic.Int64
	var errors atomic.Int64
	cancel := make(chan struct{})
	started := time.Now()
	end := started.Add(duration)
	joins := startWorkers(end, &total, &errors, cancel)
	samples := []speedSample{}
	var lastBytes int64
	lastTime := started
	peak := 0.0
	display := 0.0
	for time.Now().Before(end) {
		time.Sleep(sampleInterval)
		now := time.Now()
		current := total.Load()
		instant := mbps(current-lastBytes, now.Sub(lastTime).Seconds())
		elapsed := now.Sub(started).Milliseconds()
		samples = append(samples, speedSample{ElapsedMs: elapsed, Mbps: instant})
		display = smoothSpeed(display, instant)
		if instant > peak {
			peak = instant
		}
		if progress != nil {
			progress(phase, instant, elapsed)
		}
		lastBytes = current
		lastTime = now
	}
	close(cancel)
	done := make(chan struct{})
	go func() {
		for _, join := range joins {
			join()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(uploadDrainTimeout):
	}
	elapsedSeconds := time.Since(started).Seconds()
	average := stableAverage(samples, total.Load(), elapsedSeconds, duration)
	totalAverage := mbps(total.Load(), elapsedSeconds)
	if phase == "upload" && totalAverage > average {
		average = totalAverage
	}
	return transferResult{
		AverageMbps: average,
		PeakMbps:    peak,
		Bytes:       total.Load(),
		Errors:      errors.Load(),
		Samples:     samples,
	}
}

func isCanceled(cancel <-chan struct{}) bool {
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}

func mbps(bytes int64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	return float64(bytes) * 8 / seconds / 1_000_000
}

func smoothSpeed(previous, instant float64) float64 {
	if previous <= 0 {
		return instant
	}
	return previous + (instant-previous)*displayEmaAlpha
}

func stableAverage(samples []speedSample, totalBytes int64, elapsedSeconds float64, duration time.Duration) float64 {
	stableEndMs := int(duration.Milliseconds()) - stableCooldownMs
	if stableEndMs < stableWarmupMs {
		stableEndMs = stableWarmupMs
	}
	values := []float64{}
	for _, sample := range samples {
		if int(sample.ElapsedMs) >= stableWarmupMs && int(sample.ElapsedMs) <= stableEndMs && sample.Mbps >= 0 && !math.IsInf(sample.Mbps, 0) && !math.IsNaN(sample.Mbps) {
			values = append(values, sample.Mbps)
		}
	}
	if len(values) < 4 {
		return mbps(totalBytes, elapsedSeconds)
	}
	sort.Float64s(values)
	trim := int(float64(len(values)) * 0.1)
	if trim > (len(values)-1)/2 {
		trim = (len(values) - 1) / 2
	}
	if trim > 0 {
		values = values[trim : len(values)-trim]
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func normalizeProvince(value string) string {
	if value == "" || strings.HasSuffix(value, "省") || strings.HasSuffix(value, "市") || strings.HasSuffix(value, "自治区") || strings.HasSuffix(value, "特别行政区") {
		return value
	}
	return value + "省"
}

func normalizeCity(value string) string {
	if value == "" || strings.HasSuffix(value, "市") || strings.HasSuffix(value, "地区") || strings.HasSuffix(value, "自治州") || strings.HasSuffix(value, "盟") {
		return value
	}
	return value + "市"
}
