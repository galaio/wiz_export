package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

type ResultCode struct {
	ReturnCode    int    `json:"returnCode"`
	ReturnMessage string `json:"returnMessage"`
}

type WizUserResult struct {
	ResultCode
	Result *WizUser `json:"result"`
}

type WizUser struct {
	UserGuid    string `json:"userGuid"`
	Email       string `json:"email"`
	Mobile      string `json:"mobile"`
	DisplayName string `json:"displayName"`
	KbType      string `json:"kbType"`
	KbServer    string `json:"kbServer"`
	Token       string `json:"token"`
	KbGuid      string `json:"kbGuid"`
}

type DocListResult struct {
	ResultCode
	Result []*Doc `json:"result"`
}

type Doc struct {
	DocGuid         string `json:"docGuid"`
	Title           string `json:"title"`
	Category        string `json:"category"`
	AttachmentCount int    `json:"attachmentCount"`
	Created         int    `json:"created"`
	Accessed        int    `json:"accessed"`
	Keywords        string `json:"keywords"`
	CoverImage      string `json:"coverImage"`
}

var (
	conv     = md.NewConverter("", true, nil)
	userId   = flag.String("userId", "", "wiz userId")
	password = flag.String("password", "", "wiz password")
	output   = flag.String("output", ".", "export output")
	folders  = flag.String("folders", "", "export folders, like /日记/,/Logs/")
)

// usage
// wiz_export --output '/Users/xx/' --userId 'xx' --password 'xx' --folders '/日记/,/工作/'
func main() {
	flag.Parse()
	if *userId == "" || *password == "" || *folders == "" {
		fmt.Println("err args:")
		flag.PrintDefaults()
		panic("empty user or folders")
	}
	root := *output

	// Use the `GitHubFlavored` plugin from the `plugin` package.
	conv.Use(plugin.GitHubFlavored())
	wizUser, err := Login(*userId, *password)
	PanicErr(err)
	fmt.Printf("User info:\n\tkbServer: %s\n\tkbGuid: %s\n\ttoken: %s\n",
		wizUser.KbServer, wizUser.KbGuid, wizUser.Token)

	folderArr := strings.Split(*folders, ",")
	for _, folder := range folderArr {
		fmt.Printf("Folder info:\n\tfolder: %s\n", folder)
		if err := fetchFolder(root, wizUser, folder); err != nil {
			fmt.Println("fetchFolder err:", err)
		}

		time.Sleep(100 * time.Millisecond)
	}

}

func fetchFolder(root string, wizUser *WizUser, folder string) error {
	token := wizUser.Token
	cbs, err := Fetch(fmt.Sprintf("%s/ks/note/list/category/%s?start=0&count=200&category=%s&orderBy=created",
		wizUser.KbServer, wizUser.KbGuid, url.PathEscape(folder)), token)
	if err != nil {
		return WrapErr("fetch folder", err)
	}
	cateResult := new(DocListResult)
	if err = json.Unmarshal(cbs, cateResult); err != nil {
		return WrapErr("Unmarshal folder result", err)
	}
	if cateResult.ReturnCode != 200 {
		return WrapErr("fetch folder", err)
	}
	// make root and resource folder
	parentPath := path.Join(root, folder[1:])
	if err = os.MkdirAll(parentPath, 0755); err != nil {
		return WrapErr("MkdirAll folder", err)
	}

	if err := os.MkdirAll(path.Join(parentPath, "index_files"), 0755); err != nil {
		return WrapErr("MkdirAll index_files", err)
	}
	// read docs
	for _, doc := range cateResult.Result {
		fmt.Printf("Doc info:\n\tdocGuid: %s\n\ttitle: %s\n\tattachmentCount:%v\n",
			doc.DocGuid, doc.Title, doc.AttachmentCount)
		if err := fetchDoc(parentPath, wizUser, doc); err != nil {
			fmt.Println("fetchDoc err:", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

func fetchDoc(root string, wizUser *WizUser, doc *Doc) error {
	token := wizUser.Token
	docName := doc.Title
	if !strings.HasSuffix(docName, ".md") {
		docName = docName + ".md"
	}
	html, err := Fetch(fmt.Sprintf("%s/ks/note/view/%s/%s?objType=document",
		wizUser.KbServer, wizUser.KbGuid, doc.DocGuid), token)
	if err != nil {
		return WrapErr("fetch doc", err)
	}

	markdown, err := conv.ConvertString(string(html))
	if err != nil {
		return WrapErr("ConvertString", err)
	}
	markdown = strings.ReplaceAll(markdown, "\\", "")
	if err := os.WriteFile(path.Join(root, docName), []byte(markdown), 0644); err != nil {
		return WrapErr("WriteFile err", err)
	}

	// replace \\
	rc, err := regexp.Compile("!\\[\\]\\(index_files/(.*?)\\)")
	if err != nil {
		return WrapErr("ConvertString err", err)
	}
	matchStrs := rc.FindAllStringSubmatch(markdown, -1)

	// download resources
	fmt.Printf("Resource:\n\tcount: %v\n", len(matchStrs))
	for _, str := range matchStrs {
		fname := str[1]
		fmt.Printf("\tres: %s\n", fname)
		if err := fetchRes(path.Join(root, "index_files"), wizUser, doc, fname); err != nil {
			fmt.Println("fetchRes err:", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func fetchRes(root string, wizUser *WizUser, doc *Doc, fileName string) error {
	resPath := path.Join(root, fileName)
	_, err := os.Stat(resPath)
	// skip exist file
	if os.IsExist(err) {
		return nil
	}
	tmpData, err := Fetch(fmt.Sprintf("%s/ks/note/view/%s/%s/index_files/%s",
		wizUser.KbServer, wizUser.KbGuid, doc.DocGuid, fileName), wizUser.Token)
	if err != nil {
		return WrapErr("fetch res", err)
	}
	if err := os.WriteFile(resPath, tmpData, 0644); err != nil {
		return WrapErr("WriteFile res", err)
	}

	return nil
}

func PanicErr(err error) {
	if err != nil {
		panic(err)
	}
}

func WrapErr(errMsg string, err error) error {
	if err != nil {
		return errors.New(errMsg + ", err: " + err.Error())
	}
	return nil
}

func Login(userId, password string) (*WizUser, error) {
	body := map[string]string{"userId": userId, "password": password}
	bs, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post("https://as.wiz.cn/as/user/login", "application/json", bytes.NewReader(bs))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	rs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ur := new(WizUserResult)
	err = json.Unmarshal(rs, ur)
	if err != nil {
		return nil, err
	}
	if ur.ReturnCode != 200 {
		return nil, errors.New(ur.ReturnMessage)
	}

	return ur.Result, nil
}

func Fetch(url, token string) ([]byte, error) {
	fmt.Println("\tfetch:", url)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Wiz-Token", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	rs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return rs, nil
}
