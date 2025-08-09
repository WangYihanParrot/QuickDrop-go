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
	"strings"
	"sync"
	"time"
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
<title>QuickDrop</title>
<style>
body { font-family: Arial, sans-serif; background: #f4f4f4; margin: 0; }
.container { max-width: 800px; margin: auto; padding: 20px; }
h1 { text-align: center; }
.card { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 2px 5px rgba(0,0,0,0.1); margin-top: 20px; }
input[type=text], input[type=file] { width: 100%; padding: 8px; margin: 5px 0; }
button { padding: 8px 15px; cursor: pointer; border: none; background: #007bff; color: white; border-radius: 5px; }
button:hover { background: #0056b3; }
.copy-btn { margin-left: 10px; background: #28a745; }
.copy-btn:hover { background: #1c7c31; }
.preview-img { max-width: 150px; max-height: 150px; display: block; margin: 5px 0; border: 1px solid #ccc; border-radius: 5px; }
</style>
</head>
<body>
<div class="container">
<h1>QuickDrop</h1>
<div class="card">
<h2>上传</h2>
<form id="uploadForm" action="/upload" method="post" enctype="multipart/form-data">
	<label>文本:</label>
	<textarea name="text" rows="3" style="width:100%;"></textarea>
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
	<input type="text" name="code">
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
	document.getElementById('result').innerHTML = text;
};
function copyCode(code) {
	navigator.clipboard.writeText(code);
	alert("已复制: " + code);
}
</script>
</body>
</html>
`
	w.Write([]byte(tpl))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(maxUpload << 20)

	text := r.FormValue("text")
	files := r.MultipartForm.File["files"]

	code := fmt.Sprintf("%06d", rng.Intn(1000000))

	var fileList []FileData
	var fileNames []string

	for _, fh := range files {
		f, _ := fh.Open()
		defer f.Close()
		savePath := "./tmp_" + code + "_" + fh.Filename
		out, _ := os.Create(savePath)
		io.Copy(out, f)
		out.Close()
		fileList = append(fileList, FileData{
			FilePath: savePath,
			FileName: fh.Filename,
		})
		fileNames = append(fileNames, fh.Filename)
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
<title>查看内容</title>
<style>
body { font-family: Arial; background: #f4f4f4; padding: 20px; }
.card { background: white; padding: 20px; border-radius: 10px; max-width: 800px; margin: auto; }
.file-list { margin-top: 10px; }
.file-item { margin-bottom: 15px; border-bottom: 1px solid #ddd; padding-bottom: 10px; }
.preview-img { max-width: 200px; max-height: 200px; display: block; margin: 5px 0; }
button { padding: 5px 10px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; }
button:hover { background: #0056b3; }
.copy-btn { background: #28a745; }
.copy-btn:hover { background: #1c7c31; }
</style>
</head>
<body>
<div class="card">
<h2>QuickDrop - 提取码 {{.Code}}</h2>
{{if .Text}}
<h3>文本内容</h3>
<pre>{{.Text}}</pre>
<button class="copy-btn" onclick="navigator.clipboard.writeText('{{js .Text}}')">复制文本</button>
{{end}}
{{if .Files}}
<h3>文件列表</h3>
<div class="file-list">
{{range .Files}}
<div class="file-item">
<p><b>{{.Name}}</b></p>
{{if .IsImage}}<img src="/download?code={{.Code}}&file={{.Name}}&preview=1" class="preview-img">{{end}}
<a href="/download?code={{.Code}}&file={{.Name}}"><button>下载</button></a>
</div>
{{end}}
</div>
<a href="/download_all?code={{.Code}}"><button>全部下载 ZIP</button></a>
{{end}}
</div>
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
