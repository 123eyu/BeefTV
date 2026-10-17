package generation

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
	"testing"
)

type midjourneyImagePort struct {
	calls int
	n     string
	path  string
}

func (p *midjourneyImagePort) Intercept(req *http.Request) (bool, []byte, string, error) {
	p.calls++
	p.path = req.URL.Path
	contentType, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if contentType == "multipart/form-data" {
		if err := req.ParseMultipartForm(1 << 20); err != nil {
			return true, nil, "", err
		}
		defer req.MultipartForm.RemoveAll()
		p.n = req.FormValue("n")
	} else {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return true, nil, "", err
		}
		if body["n"] == float64(4) {
			p.n = "4"
		}
	}
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jR1sAAAAASUVORK5CYII="
	data := make([]map[string]string, 4)
	for i := range data {
		data[i] = map[string]string{"b64_json": png}
	}
	output, err := json.Marshal(map[string]any{"data": data})
	return true, output, "application/json", err
}

func midjourneyImageInput() Input {
	return Input{Mode: "image", Prompt: "a red cube", Config: Config{BaseURL: "https://beeftv.app", APIKey: "synthetic", Model: "midjourney-v8.2", InterfaceType: "openai-image", Size: "1:1", Count: "4"}}
}

func TestMidjourneySubmitsOneFourImageRequest(t *testing.T) {
	for _, reference := range []bool{false, true} {
		input := midjourneyImageInput()
		if reference {
			input.ReferenceImages = []Media{{DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jR1sAAAAASUVORK5CYII="}}
		}
		port := &midjourneyImagePort{}
		result, err := RunImageTask(WithRuntime(context.Background(), Runtime{Images: port}), input)
		if err != nil {
			t.Fatal(err)
		}
		path := "/v1/images/generations"
		if reference {
			path = "/v1/images/edits"
		}
		if port.calls != 1 || port.n != "4" || port.path != path || len(result["images"].([]map[string]string)) != 4 {
			t.Fatalf("wire=%#v result=%#v", port, result)
		}
	}
}

func TestExecuteMidjourneyUsesOneFourImageRequest(t *testing.T) {
	port := &midjourneyImagePort{}
	ctx := WithRuntime(context.Background(), taskRuntime(func(runtime *Runtime) { runtime.Images = port }))
	result, err := Execute(ctx, midjourneyImageInput())
	if err != nil {
		t.Fatal(err)
	}
	if port.calls != 1 || port.n != "4" || len(result["images"].([]map[string]string)) != 4 {
		t.Fatalf("wire=%#v result=%#v", port, result)
	}
}

func TestMidjourneyRejectsUnsupportedInputBeforeSubmit(t *testing.T) {
	for _, change := range []func(*Input){
		func(i *Input) { i.Config.Count = "1" },
		func(i *Input) { i.Config.Size = "16:9" },
		func(i *Input) { i.Config.Quality = "high" },
		func(i *Input) { i.Mask = &Media{} },
		func(i *Input) { i.Config.TransparentBackground = "true" },
		func(i *Input) { i.ReferenceImages = make([]Media, 2) },
		func(i *Input) { i.Prompt = strings.Repeat("字", 2001) },
	} {
		input, port := midjourneyImageInput(), &midjourneyImagePort{}
		change(&input)
		_, err := RunImageTask(WithRuntime(context.Background(), Runtime{Images: port}), input)
		if err == nil || port.calls != 0 {
			t.Fatalf("invalid input dispatched: calls=%d err=%v", port.calls, err)
		}
	}
}
