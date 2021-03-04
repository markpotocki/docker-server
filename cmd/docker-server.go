package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type DockerBuildRequestMap struct {
	requests map[string]string
	mutex *sync.Mutex
}

type DockerBuildFile struct {
	name string
	tag string
	ports []int
	image io.Reader
}

type dockerBuildRequest struct {
	id string
	buildFile DockerBuildFile
}

type callbackID string

const (
	applicationVersion = "0.0.1"
	applicationName = "Beagle -- Project Ysondre"
	applicationHost = ""
	applicationPort = 7770
	imageFileFormKey = "dockerImage"
	imageNameFormKey = "name"
	imageTagFormKey = "tag"
	imagePortsFormKey = "ports"
	statusRunning = "Running"
	statusFailed = "Failed"
	statusSuccess = "Done"
	urlCallback = "/status"
	uuidKeySize = 16
)

var DockerRequests = DockerBuildRequestMap {
	requests: make(map[string]string),
	mutex: &sync.Mutex{},
}

var dockerRequestChan = make(chan dockerBuildRequest) // TODO add buffer

func main() {
	log.Println(applicationName)
	log.Printf("Version %s\n", applicationVersion)
	serverErrorChan := make(chan error)
	go func() {
		log.Printf("Starting server with address %s:%d", applicationHost, applicationPort)
		serverErrorChan <- http.ListenAndServe(fmt.Sprintf("%s:%d", applicationHost, applicationPort), http.HandlerFunc(imageHandlerFunc))
	}()

	select {
		case <-serverErrorChan:
			log.Fatalln("server has shutdown", serverErrorChan)
	}
}

func imageBuildStatusGETHandlerFunc(w http.ResponseWriter, r *http.Request) {
	
}

// Submission Handler
func imageHandlerFunc(w http.ResponseWriter ,r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			http.Error(w, fmt.Sprint(r), http.StatusBadRequest)
		}
	}()
	// only want POSTs
	if r.Method != http.MethodPost {
		http.Error(w, "only POST is supported, got " + r.Method, http.StatusMethodNotAllowed)
		return
	}
	// let's load the file from the request
	// form is parsed contained the file
	dockerFile, _, err := r.FormFile(imageFileFormKey)
	if err != nil {
		log.Panicln("unable to get docker file", err)
	}
	// we'll need the name and tag as well in the form
	imageName := r.PostFormValue(imageNameFormKey)
	if err := validateFormInputs(imageName); err != nil {
		log.Panicln(err)
	}
	imageTag := r.PostFormValue(imageTagFormKey)
	if err := validateFormInputs(imageTag); err != nil {
		log.Panicln(err)
	}
	// process the ports list
	imagePorts := r.PostFormValue(imagePortsFormKey) // this is optional
	imagePortsSlice := strings.Split(imagePorts, ",")
	imagePortsIntSlice := make([]int, len(imagePortsSlice))
	for _, port := range imagePortsSlice {
		intValue, err := strconv.ParseInt(port, 10, 64)
		if err != nil {
			log.Panicln(err)
		}
		imagePortsIntSlice = append(imagePortsIntSlice, int(intValue))
	}
	dockerBuildFile := DockerBuildFile {
		name: imageName,
		tag: imageTag,
		ports: imagePortsIntSlice,
		image: dockerFile,
	}

	// execute the docker build and return the callback url
	id := executeDockerBuildFromImportFile(dockerBuildFile)
	http.Redirect(w, r, fmt.Sprintf("%s/%s", urlCallback, id), http.StatusFound)
}

func validateFormInputs(formValue string) error {
	if formValue == "" {
		// invalid
		return errors.New("invalid form input")
	}
	return nil
}

func executeDockerBuildFromImportFile(dockerFile DockerBuildFile) callbackID {
	id := generateUUID()
	go func() {
		defer func() {
			if r := recover(); r != nil {

			}
		}()
		DockerRequests.set(id, statusRunning)
		imageName := fmt.Sprintf("%s:%s", dockerFile.name, dockerFile.tag)
		// docker saved with save, use docker load for the file
		log.Printf("[%s] loading docker archive", id)
		loadDockerImageCmd := exec.Command("docker", "load")
		loadDockerImageCmd.Stdin = dockerFile.image
		loadDockerImageCmd.Stderr = log.Writer()
		// run import command and check for errors
		if err := loadDockerImageCmd.Run(); err != nil {
			DockerRequests.set(id, statusFailed)
			log.Println("import the docker image failed")
			log.Panicln(err)
		}
		// load is good, we can now run the program
		// load the ports list
		log.Printf("[%s] running command", id)
		runDockerImageCmd := exec.Command("docker", "run", "-d", "-P", "--name", id, imageName)
		runDockerImageCmd.Stderr = log.Writer()
		if err := runDockerImageCmd.Run(); err != nil {
			DockerRequests.set(id, statusFailed)
			log.Panicln(err)
		}
		// no errors, we didn't panic, send to channel
		DockerRequests.set(id, statusSuccess)
		dockerRequestChan <- dockerBuildRequest{
			id: id,
			buildFile: dockerFile,
		}
	}()

	// async out of the go func, return the request id so a callback url can check status
	return callbackID(id)
}

func (dbrm *DockerBuildRequestMap) set(key string, value string) {
	dbrm.mutex.Lock()
	defer dbrm.mutex.Unlock()
	dbrm.requests[key] = value
}

func generateUUID() string {
	const letters = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	ret := make([]byte, uuidKeySize)
	for i := 0; i < len(ret); i++ {
		num := rand.Int() % len(letters)
		ret[i] = letters[num]
	}
	return string(ret)
}
