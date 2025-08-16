package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type FileData struct {
	FilePath string
	FileName string
}

type Item struct {
	Files    []FileData
	Text     string
	ExpireAt time.Time
}

var (
	store     = make(map[string]Item)
	mu        sync.Mutex
	port      int
	maxUpload int64
)

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// cleanFileName 清理文件名，移除非法字符并限制长度
func cleanFileName(filename string) string {
	if filename == "" {
		return "unnamed_file"
	}

	// 获取文件扩展名
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)

	// 移除或替换非法字符
	invalidChars := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	name = invalidChars.ReplaceAllString(name, "_")

	// 移除前后空格和点
	name = strings.Trim(name, " .")

	// 如果清理后名字为空，使用默认名称
	if name == "" {
		name = "unnamed_file"
	}

	// 限制文件名长度（不包括扩展名）
	maxNameLength := 100
	if utf8.RuneCountInString(name) > maxNameLength {
		// 截断到指定长度
		runes := []rune(name)
		if len(runes) > maxNameLength {
			name = string(runes[:maxNameLength])
		}
	}

	// 重新组合文件名
	cleanedName := name + ext

	// 最终检查总长度
	maxTotalLength := 150
	if utf8.RuneCountInString(cleanedName) > maxTotalLength {
		// 如果总长度还是太长，进一步截断名称部分
		availableLength := maxTotalLength - utf8.RuneCountInString(ext)
		if availableLength > 0 {
			runes := []rune(name)
			if len(runes) > availableLength {
				name = string(runes[:availableLength])
			}
		} else {
			name = "file"
		}
		cleanedName = name + ext
	}

	return cleanedName
}

func main() {
	flag.IntVar(&port, "port", 8080, "server port")
	flag.Int64Var(&maxUpload, "maxsize", 768, "max upload size in MB")
	flag.Parse()

	go cleaner()

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/view", viewHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/download_all", downloadAllHandler)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("QuickDrop (budgie) running on %s, max upload %d MB\n", addr, maxUpload)
	http.ListenAndServe(addr, nil)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tpl := `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<title>QuickDrop</title>
<style>
body { 
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; 
	background: #f4f4f4; 
	margin: 0; 
	padding: 10px;
	font-size: 16px;
}
.container { 
	max-width: 800px; 
	margin: auto; 
	padding: 20px; 
}
h1 { 
	text-align: center; 
	font-size: 2em;
	margin-bottom: 20px;
}
h2 {
	font-size: 1.3em;
	margin-bottom: 15px;
}
.card { 
	background: white; 
	padding: 20px; 
	border-radius: 10px; 
	box-shadow: 0 2px 5px rgba(0,0,0,0.1); 
	margin-bottom: 20px; 
}
input[type=text], input[type=file], textarea { 
	width: 100%; 
	padding: 12px; 
	margin: 8px 0; 
	border: 1px solid #ddd;
	border-radius: 5px;
	font-size: 16px;
	box-sizing: border-box;
}
textarea {
	resize: vertical;
	min-height: 80px;
}
button { 
	padding: 12px 20px; 
	cursor: pointer; 
	border: none; 
	background: #007bff; 
	color: white; 
	border-radius: 5px;
	font-size: 16px;
	width: 100%;
	margin-top: 10px;
}
button:hover { 
	background: #0056b3; 
}
.copy-btn { 
	margin-left: 10px; 
	background: #28a745;
	width: auto;
	padding: 8px 15px;
	font-size: 14px;
}
.copy-btn:hover { 
	background: #1c7c31; 
}
label {
	display: block;
	margin-bottom: 5px;
	font-weight: bold;
	color: #333;
}
#result {
	margin-top: 15px;
	padding: 15px;
	background: #d4edda;
	border: 1px solid #c3e6cb;
	border-radius: 5px;
	display: none;
}

/* 移动端优化 */
@media (max-width: 768px) {
	body {
		padding: 5px;
		font-size: 16px;
	}
	.container {
		padding: 10px;
	}
	.card {
		padding: 15px;
		margin-bottom: 15px;
	}
	h1 {
		font-size: 1.8em;
	}
	h2 {
		font-size: 1.2em;
	}
	.copy-btn {
		width: 100%;
		margin: 10px 0 0 0;
	}
	button {
		padding: 15px;
		font-size: 16px;
	}
}

@media (max-width: 480px) {
	.container {
		padding: 5px;
	}
	.card {
		padding: 12px;
	}
	h1 {
		font-size: 1.6em;
	}
}
</style>
</head>
<body>
<div class="container">
<h1>QuickDrop</h1>
<div class="card">
<h2>上传</h2>
<form id="uploadForm" action="/upload" method="post" enctype="multipart/form-data">
	<label>文本:</label>
	<textarea name="text" rows="3" placeholder="输入要分享的文本内容..."></textarea>
	<label>文件(可多选):</label>
	<input type="file" name="files" multiple>
	<button type="submit">上传</button>
</form>
<div id="result"></div>
</div>

<div class="card">
<h2>提取</h2>
<form action="/view" method="get">
	<label>提取码:</label>
	<input type="text" name="code" placeholder="输入6位数字提取码">
	<button type="submit">查看</button>
</form>
</div>
</div>
<script>
document.getElementById('uploadForm').onsubmit = async function(e) {
	e.preventDefault();
	let formData = new FormData(this);
	let res = await fetch('/upload', { method: 'POST', body: formData });
	let text = await res.text();
	let resultDiv = document.getElementById('result');
	resultDiv.innerHTML = text;
	resultDiv.style.display = 'block';
};
function copyCode(code) {
	if (navigator.clipboard) {
		navigator.clipboard.writeText(code).then(() => {
			alert("已复制: " + code);
		});
	} else {
		// 兼容旧浏览器
		let textArea = document.createElement("textarea");
		textArea.value = code;
		document.body.appendChild(textArea);
		textArea.select();
		document.execCommand('copy');
		document.body.removeChild(textArea);
		alert("已复制: " + code);
	}
}
</script>
</body>
</html>
`
	w.Write([]byte(tpl))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(maxUpload << 20)
	if err != nil {
		http.Error(w, "请求解析失败", http.StatusBadRequest)
		return
	}

	text := r.FormValue("text")
	files := r.MultipartForm.File["files"]

	code := fmt.Sprintf("%06d", rng.Intn(1000000))

	var fileList []FileData
	var fileNames []string

	for _, fh := range files {
		if fh == nil {
			continue // 跳过空的文件句柄
		}

		// 清理文件名
		originalName := fh.Filename
		cleanedName := cleanFileName(originalName)

		f, err := fh.Open()
		if err != nil {
			fmt.Printf("无法打开文件 %s: %v\n", originalName, err)
			continue
		}
		defer f.Close()

		savePath := "./tmp_" + code + "_" + cleanedName
		out, err := os.Create(savePath)
		if err != nil {
			fmt.Printf("无法创建文件 %s: %v\n", savePath, err)
			continue
		}

		_, err = io.Copy(out, f)
		out.Close()
		if err != nil {
			fmt.Printf("文件复制失败 %s: %v\n", cleanedName, err)
			os.Remove(savePath) // 清理失败的文件
			continue
		}

		fileList = append(fileList, FileData{
			FilePath: savePath,
			FileName: cleanedName,
		})
		fileNames = append(fileNames, cleanedName)

		// 如果文件名被修改了，记录日志
		if originalName != cleanedName {
			fmt.Printf("文件名已清理: %s -> %s\n", originalName, cleanedName)
		}
	}

	mu.Lock()
	store[code] = Item{
		Files:    fileList,
		Text:     text,
		ExpireAt: time.Now().Add(5 * time.Minute),
	}
	mu.Unlock()

	// 日志
	fmt.Printf("[%s] %s 上传 文件: %v 文本: %q\n",
		time.Now().Format("2006-01-02 15:04:05"), code, fileNames, text)

	// 返回带复制按钮的 HTML
	fmt.Fprintf(w, `<div>上传成功，提取码: <b>%s</b>
	<button class="copy-btn" onclick="copyCode('%s')">复制</button>
	<p>5分钟内有效</p>
	<a href="/view?code=%s" target="_blank">立即查看</a></div>`, code, code, code)
}

func viewHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	mu.Lock()
	item, ok := store[code]
	mu.Unlock()

	if !ok || time.Now().After(item.ExpireAt) {
		http.Error(w, "提取码无效或已过期", http.StatusNotFound)
		return
	}

	type FileView struct {
		Name    string
		IsImage bool
		Code    string
	}
	var fileViews []FileView
	for _, f := range item.Files {
		ext := strings.ToLower(filepath.Ext(f.FileName))
		isImg := ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif"
		fileViews = append(fileViews, FileView{
			Name:    f.FileName,
			IsImage: isImg,
			Code:    code,
		})
	}

	tpl := `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<title>查看内容</title>
<style>
body { 
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; 
	background: #f4f4f4; 
	padding: 10px;
	margin: 0;
	font-size: 16px;
}
.card { 
	background: white; 
	padding: 20px; 
	border-radius: 10px; 
	max-width: 800px; 
	margin: auto;
	box-shadow: 0 2px 5px rgba(0,0,0,0.1);
}
.file-list { 
	margin-top: 15px; 
}
.file-item { 
	margin-bottom: 20px; 
	border-bottom: 1px solid #ddd; 
	padding-bottom: 15px; 
}
.preview-img { 
	max-width: 100%; 
	max-height: 300px; 
	display: block; 
	margin: 10px 0;
	border-radius: 5px;
	box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}
button { 
	padding: 10px 15px; 
	background: #007bff; 
	color: white; 
	border: none; 
	border-radius: 5px; 
	cursor: pointer;
	font-size: 16px;
	margin: 5px 5px 5px 0;
}
button:hover { 
	background: #0056b3; 
}
.copy-btn { 
	background: #28a745; 
}
.copy-btn:hover { 
	background: #1c7c31; 
}
h2 {
	color: #333;
	margin-bottom: 20px;
	font-size: 1.3em;
}
h3 {
	color: #333;
	margin: 20px 0 10px 0;
	font-size: 1.1em;
}
pre {
	background: #f8f9fa;
	padding: 15px;
	border-radius: 5px;
	border: 1px solid #e9ecef;
	white-space: pre-wrap;
	word-wrap: break-word;
	font-size: 14px;
	line-height: 1.4;
}
.file-name {
	font-weight: bold;
	color: #333;
	margin-bottom: 10px;
	word-break: break-all;
}
.download-all-btn {
	background: #17a2b8;
	width: 100%;
	margin-top: 15px;
	padding: 12px;
}
.download-all-btn:hover {
	background: #138496;
}

/* 移动端优化 */
@media (max-width: 768px) {
	body {
		padding: 5px;
	}
	.card {
		padding: 15px;
		margin: 5px;
	}
	h2 {
		font-size: 1.2em;
		text-align: center;
	}
	h3 {
		font-size: 1.1em;
	}
	button {
		width: 100%;
		padding: 12px;
		margin: 5px 0;
		font-size: 16px;
	}
	pre {
		font-size: 13px;
		padding: 12px;
	}
	.file-item {
		margin-bottom: 15px;
		padding-bottom: 12px;
	}
	.preview-img {
		max-height: 250px;
	}
}

@media (max-width: 480px) {
	.card {
		padding: 12px;
	}
	h2 {
		font-size: 1.1em;
	}
	pre {
		font-size: 12px;
		padding: 10px;
	}
}
</style>
</head>
<body>
<div class="card">
<h2>QuickDrop - 提取码 {{.Code}}</h2>
{{if .Text}}
<h3>文本内容</h3>
<pre>{{.Text}}</pre>
<button class="copy-btn" onclick="copyText('{{js .Text}}')">复制文本</button>
{{end}}
{{if .Files}}
<h3>文件列表</h3>
<div class="file-list">
{{range .Files}}
<div class="file-item">
<div class="file-name">{{.Name}}</div>
{{if .IsImage}}<img src="/download?code={{.Code}}&file={{.Name}}&preview=1" class="preview-img" loading="lazy">{{end}}
<a href="/download?code={{.Code}}&file={{.Name}}"><button>下载 {{.Name}}</button></a>
</div>
{{end}}
</div>
<a href="/download_all?code={{.Code}}"><button class="download-all-btn">全部下载 ZIP</button></a>
{{end}}
</div>
<script>
function copyText(text) {
	if (navigator.clipboard) {
		navigator.clipboard.writeText(text).then(() => {
			alert("文本已复制到剪贴板");
		});
	} else {
		// 兼容旧浏览器
		let textArea = document.createElement("textarea");
		textArea.value = text;
		document.body.appendChild(textArea);
		textArea.select();
		document.execCommand('copy');
		document.body.removeChild(textArea);
		alert("文本已复制到剪贴板");
	}
}
</script>
</body>
</html>
`

	t := template.Must(template.New("view").Parse(tpl))
	t.Execute(w, struct {
		Code  string
		Text  string
		Files []FileView
	}{
		Code:  code,
		Text:  item.Text,
		Files: fileViews,
	})
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	fileName := r.URL.Query().Get("file")
	preview := r.URL.Query().Get("preview")

	mu.Lock()
	item, ok := store[code]
	mu.Unlock()
	if !ok || time.Now().After(item.ExpireAt) {
		http.Error(w, "提取码无效或已过期", http.StatusNotFound)
		return
	}

	for _, f := range item.Files {
		if f.FileName == fileName {
			if preview == "1" {
				http.ServeFile(w, r, f.FilePath)
			} else {
				w.Header().Set("Content-Disposition", "attachment; filename=\""+f.FileName+"\"")
				http.ServeFile(w, r, f.FilePath)
			}
			return
		}
	}
	http.Error(w, "文件不存在", http.StatusNotFound)
}

func downloadAllHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")

	mu.Lock()
	item, ok := store[code]
	mu.Unlock()
	if !ok || time.Now().After(item.ExpireAt) {
		http.Error(w, "提取码无效或已过期", http.StatusNotFound)
		return
	}

	zipPath := "./tmp_" + code + "_all.zip"
	zipFile, _ := os.Create(zipPath)
	defer zipFile.Close()
	zipWriter := zip.NewWriter(zipFile)
	for _, f := range item.Files {
		fw, _ := zipWriter.Create(f.FileName)
		fileContent, _ := os.Open(f.FilePath)
		io.Copy(fw, fileContent)
		fileContent.Close()
	}
	zipWriter.Close()

	w.Header().Set("Content-Disposition", "attachment; filename=\"all_files.zip\"")
	http.ServeFile(w, r, zipPath)
	os.Remove(zipPath)
}

func cleaner() {
	for {
		time.Sleep(30 * time.Second)
		mu.Lock()
		for code, item := range store {
			if time.Now().After(item.ExpireAt) {
				for _, f := range item.Files {
					os.Remove(f.FilePath)
				}
				delete(store, code)
				fmt.Printf("[%s] %s 销毁!\n", time.Now().Format("2006-01-02 15:04:05"), code)
			}
		}
		mu.Unlock()
	}
}
