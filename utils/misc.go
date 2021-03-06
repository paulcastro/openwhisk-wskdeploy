/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package utils

import (
	"archive/zip"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/hokaccha/go-prettyjson"
	"github.com/openwhisk/openwhisk-client-go/whisk"
	"github.com/openwhisk/openwhisk-wskdeploy/wski18n"
)

// ActionRecord is a container to keep track of
// a whisk action struct and a location filepath we use to
// map files and manifest declared actions
type ActionRecord struct {
	Action      *whisk.Action
	Packagename string
	Filepath    string
}

type TriggerRecord struct {
	Trigger     *whisk.Trigger
	Packagename string
}

type RuleRecord struct {
	Rule        *whisk.Rule
	Packagename string
}

// Bind the action name and the ExposedUrl
type ActionExposedURLBinding struct {
	ActionName string //action name
	ExposedUrl string //exposedUrl in format method/baseurl/relativeurl
}

// Utility to convert hostname to URL object
func GetURLBase(host string) (*url.URL, error) {

	urlBase := fmt.Sprintf("%s/api", host)
	url, err := url.Parse(urlBase)

	if len(url.Scheme) == 0 || len(url.Host) == 0 {
		urlBase = fmt.Sprintf("https://%s/api", host)
		url, err = url.Parse(urlBase)
	}

	return url, err
}

func GetHomeDirectory() string {
	usr, err := user.Current()
	Check(err)

	return usr.HomeDir
}

// Potentially complex structures(such as DeploymentApplication, DeploymentPackage)
// could implement those interface which is convenient for put, get subtract in
// containers etc.
type Comparable interface {
	HashCode() uint32
	Equals() bool
}

func IsFeedAction(trigger *whisk.Trigger) (string, bool) {
	for _, annotation := range trigger.Annotations {
		if annotation.Key == "feed" {
			return annotation.Value.(string), true
		}
	}

	return "", false
}

func IsJSON(s string) (interface{}, bool) {
	var js interface{}
	if json.Unmarshal([]byte(s), &js) == nil {
		return js, true
	}
	return nil, false

}

func PrettyJSON(j interface{}) string {
	formatter := prettyjson.NewFormatter()
	bytes, err := formatter.Marshal(j)
	Check(err)
	return string(bytes)
}

// Common utilities

// Prompt for user input
func Ask(reader *bufio.Reader, question string, def string) string {
	fmt.Print(question + " (" + def + "): ")
	answer, _ := reader.ReadString('\n')
	len := len(answer)
	if len == 1 {
		return def
	}
	return answer[:len-1]
}

// Get the env variable value by key.
// Get the env variable if the key is start by $
func GetEnvVar(key interface{}) interface{} {
	if reflect.TypeOf(key).String() == "string" {
		if strings.HasPrefix(key.(string), "$") {
			envkey := strings.Split(key.(string), "$")[1]
			value := os.Getenv(envkey)
			if value != "" {
				return value
			}
			return envkey
		}
		return key.(string)
	}
	return key
}

var kindToJSON []string = []string{"", "boolean", "integer", "integer", "integer", "integer", "integer", "integer", "integer", "integer",
	"integer", "integer", "integer", "number", "number", "number", "number", "array", "", "", "", "object", "", "", "string", "", ""}

// Gets JSON type name
func GetJSONType(j interface{}) string {
	fmt.Print(reflect.TypeOf(j).Kind())
	return kindToJSON[reflect.TypeOf(j).Kind()]
}

// zip whole folder to a zip file
func CreateFolderZip(src, des string) error {
	zippedFile, err := os.Create(des)
	Check(err)
	defer zippedFile.Close()

	zipWritter := zip.NewWriter(zippedFile)
	defer zipWritter.Close()

	sinfo, err := os.Stat(src)
	Check(err)

	var basedir string
	if sinfo.IsDir() {
		basedir = filepath.Base(src)
	}

	filepath.Walk(src, func(path string, finfo os.FileInfo, err error) error {
		Check(err)

		header, err := zip.FileInfoHeader(finfo)
		Check(err)

		if basedir != "" {
			header.Name = filepath.Join(basedir, strings.TrimPrefix(path, src))
		}

		if finfo.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := zipWritter.CreateHeader(header)
		Check(err)

		file, err := os.Open(path)
		Check(err)
		defer file.Close()
		_, err = io.Copy(writer, file)
		Check(err)
		return err
	})

	return err
}

// zip given files to a zip file.
func CreateFilesZip(filename string, files []string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	zipwriter := zip.NewWriter(file)
	defer zipwriter.Close()
	for _, name := range files {
		if err := writeFileToZip(zipwriter, name); err != nil {
			return err
		}
	}
	return nil
}

func writeFileToZip(zipwriter *zip.Writer, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	finfo, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(finfo)
	if err != nil {
		return err
	}
	//add some filter logic if necessary
	//filter(file)
	writer, err := zipwriter.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func filter(filename string) interface{} {
	//To do
	return nil
}

// below codes is from wsk cli with tiny adjusts.
func GetExec(artifact string, kind string, isDocker bool, mainEntry string) (*whisk.Exec, error) {
	var err error
	var code string
	var content []byte
	var exec *whisk.Exec

	ext := filepath.Ext(artifact)
	exec = new(whisk.Exec)

	if !isDocker || ext == ".zip" {
		content, err = new(ContentReader).ReadLocal(artifact)
		Check(err)
		code = string(content)
		exec.Code = &code
	}

	if len(kind) > 0 {
		exec.Kind = kind
	} else if isDocker {
		exec.Kind = "blackbox"
		if ext != ".zip" {
			exec.Image = artifact
		} else {
			exec.Image = "openwhisk/dockerskeleton"
		}
	} else if ext == ".swift" {
		exec.Kind = "swift:default"
	} else if ext == ".js" {
		exec.Kind = "nodejs:default"
	} else if ext == ".py" {
		exec.Kind = "python:default"
	} else if ext == ".jar" {
		exec.Kind = "java:default"
		exec.Code = nil
	} else {
		if ext == ".zip" {
			return nil, zipKindError()
		} else {
			return nil, extensionError(ext)
		}
	}

	// Error if entry point is not specified for Java
	if len(mainEntry) != 0 {
		exec.Main = mainEntry
	} else {
		if exec.Kind == "java" {
			return nil, javaEntryError()
		}
	}

	// Base64 encode the zip file content
	if ext == ".zip" {
		code = base64.StdEncoding.EncodeToString([]byte(code))
		exec.Code = &code
	}

	return exec, nil
}

func zipKindError() error {
	errMsg := wski18n.T("creating an action from a .zip artifact requires specifying the action kind explicitly")

	return errors.New(errMsg)
}

func extensionError(extension string) error {
	errMsg := wski18n.T(
		"'{{.name}}' is not a supported action runtime",
		map[string]interface{}{
			"name": extension,
		})

	return errors.New(errMsg)
}

func javaEntryError() error {
	errMsg := wski18n.T("Java actions require --main to specify the fully-qualified name of the main class")

	return errors.New(errMsg)
}

//for web action support, code from wsk cli with tiny adjustments
const WEB_EXPORT_ANNOT = "web-export"
const RAW_HTTP_ANNOT = "raw-http"
const FINAL_ANNOT = "final"

func WebAction(webMode string, annotations whisk.KeyValueArr, entityName string, fetch bool) (whisk.KeyValueArr, error) {
	switch strings.ToLower(webMode) {
	case "yes":
		fallthrough
	case "true":
		return webActionAnnotations(fetch, annotations, entityName, addWebAnnotations)
	case "no":
		fallthrough
	case "false":
		return webActionAnnotations(fetch, annotations, entityName, deleteWebAnnotations)
	case "raw":
		return webActionAnnotations(fetch, annotations, entityName, addRawAnnotations)
	default:
		return nil, errors.New(webMode)
	}
}

type WebActionAnnotationMethod func(annotations whisk.KeyValueArr) whisk.KeyValueArr

func webActionAnnotations(
	fetchAnnotations bool,
	annotations whisk.KeyValueArr,
	entityName string,
	webActionAnnotationMethod WebActionAnnotationMethod) (whisk.KeyValueArr, error) {
	if annotations != nil || !fetchAnnotations {
		annotations = webActionAnnotationMethod(annotations)
	}

	return annotations, nil
}

func addWebAnnotations(annotations whisk.KeyValueArr) whisk.KeyValueArr {
	annotations = deleteWebAnnotationKeys(annotations)
	annotations = addKeyValue(WEB_EXPORT_ANNOT, true, annotations)
	annotations = addKeyValue(RAW_HTTP_ANNOT, false, annotations)
	annotations = addKeyValue(FINAL_ANNOT, true, annotations)

	return annotations
}

func deleteWebAnnotations(annotations whisk.KeyValueArr) whisk.KeyValueArr {
	annotations = deleteWebAnnotationKeys(annotations)
	annotations = addKeyValue(WEB_EXPORT_ANNOT, false, annotations)
	annotations = addKeyValue(RAW_HTTP_ANNOT, false, annotations)
	annotations = addKeyValue(FINAL_ANNOT, false, annotations)

	return annotations
}

func addRawAnnotations(annotations whisk.KeyValueArr) whisk.KeyValueArr {
	annotations = deleteWebAnnotationKeys(annotations)
	annotations = addKeyValue(WEB_EXPORT_ANNOT, true, annotations)
	annotations = addKeyValue(RAW_HTTP_ANNOT, true, annotations)
	annotations = addKeyValue(FINAL_ANNOT, true, annotations)

	return annotations
}

func deleteWebAnnotationKeys(annotations whisk.KeyValueArr) whisk.KeyValueArr {
	annotations = deleteKey(WEB_EXPORT_ANNOT, annotations)
	annotations = deleteKey(RAW_HTTP_ANNOT, annotations)
	annotations = deleteKey(FINAL_ANNOT, annotations)

	return annotations
}

func deleteKey(key string, keyValueArr whisk.KeyValueArr) whisk.KeyValueArr {
	for i := 0; i < len(keyValueArr); i++ {
		if keyValueArr[i].Key == key {
			keyValueArr = append(keyValueArr[:i], keyValueArr[i+1:]...)
			break
		}
	}

	return keyValueArr
}

func addKeyValue(key string, value interface{}, keyValueArr whisk.KeyValueArr) whisk.KeyValueArr {
	keyValue := whisk.KeyValue{
		Key:   key,
		Value: value,
	}

	return append(keyValueArr, keyValue)
}
