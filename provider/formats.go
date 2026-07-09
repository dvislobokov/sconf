package provider

import (
	"bytes"
	"encoding/json"

	toml "github.com/pelletier/go-toml/v2"
	yaml "gopkg.in/yaml.v3"
)

// JSONFile создаёт источник из JSON-файла.
func JSONFile(path string, opts ...FileOption) *fileProvider {
	return newFileProvider(path, parseJSON, opts)
}

// YAMLFile создаёт источник из YAML-файла.
func YAMLFile(path string, opts ...FileOption) *fileProvider {
	return newFileProvider(path, parseYAML, opts)
}

// TOMLFile создаёт источник из TOML-файла.
func TOMLFile(path string, opts ...FileOption) *fileProvider {
	return newFileProvider(path, parseTOML, opts)
}

func parseJSON(data []byte) (map[string]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]string{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // сохраняем исходное числовое представление
	var root interface{}
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	return flattenTree(root), nil
}

func parseYAML(data []byte) (map[string]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]string{}, nil
	}
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return flattenTree(root), nil
}

func parseTOML(data []byte) (map[string]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]string{}, nil
	}
	var root interface{}
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return flattenTree(root), nil
}
