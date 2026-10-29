// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ClassConfigSpecDocument edits individual ConfigSpec fields without decoding
// unrelated values into vSphere types or losing their JSON representation.
type ClassConfigSpecDocument struct {
	original json.RawMessage
	root     map[string]json.RawMessage
	dirty    bool
}

// ParseClassConfigSpec parses a class ConfigSpec while preserving unknown fields.
func ParseClassConfigSpec(raw json.RawMessage) (*ClassConfigSpecDocument, error) {
	doc := &ClassConfigSpecDocument{original: append(json.RawMessage(nil), raw...)}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc.root); err != nil {
			return nil, fmt.Errorf("decode class ConfigSpec: %w", err)
		}
	}
	if doc.root == nil {
		doc.root = map[string]json.RawMessage{}
	}
	return doc, nil
}

// Get decodes a dot-separated field path, reporting whether the field exists.
// It does not remove the field; unsupported values can remain in ConfigSpec.
func (d *ClassConfigSpecDocument) Get(path string, out any) (bool, error) {
	parts := strings.Split(path, ".")
	object := d.root
	for i, part := range parts {
		raw, ok := object[part]
		if !ok {
			return false, nil
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false, nil
		}
		if i == len(parts)-1 {
			if err := json.Unmarshal(raw, out); err != nil {
				return false, fmt.Errorf("decode class ConfigSpec %s: %w", path, err)
			}
			return true, nil
		}
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err != nil {
			return false, fmt.Errorf("decode class ConfigSpec parent of %s: %w", path, err)
		}
		object = child
	}
	return false, nil
}

// Set replaces a field while preserving unrelated fields and sibling values.
func (d *ClassConfigSpecDocument) Set(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode class ConfigSpec %s: %w", path, err)
	}
	if err := d.edit(strings.Split(path, "."), raw, false); err != nil {
		return err
	}
	if _, ok := d.root["_typeName"]; !ok {
		d.root["_typeName"] = json.RawMessage(`"VirtualMachineConfigSpec"`)
	}
	d.dirty = true
	return nil
}

// Delete removes one field and any parent object left wholly empty.
func (d *ClassConfigSpecDocument) Delete(path string) error {
	if err := d.edit(strings.Split(path, "."), nil, true); err != nil {
		return err
	}
	d.dirty = true
	return nil
}

func (d *ClassConfigSpecDocument) edit(path []string, value json.RawMessage, remove bool) error {
	return editClassConfigSpecField(d.root, path, value, remove)
}

func editClassConfigSpecField(object map[string]json.RawMessage, path []string, value json.RawMessage, remove bool) error {
	if len(path) == 1 {
		if remove {
			delete(object, path[0])
		} else {
			object[path[0]] = value
		}
		return nil
	}
	child := map[string]json.RawMessage{}
	if raw, ok := object[path[0]]; ok {
		if err := json.Unmarshal(raw, &child); err != nil {
			return fmt.Errorf("decode class ConfigSpec object %s: %w", path[0], err)
		}
	}
	if child == nil {
		child = map[string]json.RawMessage{}
	}
	if err := editClassConfigSpecField(child, path[1:], value, remove); err != nil {
		return err
	}
	if remove && len(child) == 0 {
		delete(object, path[0])
		return nil
	}
	raw, err := json.Marshal(child)
	if err != nil {
		return fmt.Errorf("encode class ConfigSpec object %s: %w", path[0], err)
	}
	object[path[0]] = raw
	return nil
}

// Marshal returns an isolated representation, preserving untouched documents
// byte for byte and omission when no residual fields remain.
func (d *ClassConfigSpecDocument) Marshal() (json.RawMessage, error) {
	if !d.dirty {
		return append(json.RawMessage(nil), d.original...), nil
	}
	if len(d.root) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(d.root)
	if err != nil {
		return nil, fmt.Errorf("encode class ConfigSpec: %w", err)
	}
	return raw, nil
}

// PromoteClassConfigSpec moves supported legacy ConfigSpec presets into typed
// policies. Each category adds one explicit mapping in this dispatcher.
func PromoteClassConfigSpec(spec *VirtualMachineClassSpec) error {
	doc, err := ParseClassConfigSpec(spec.ConfigSpec)
	if err != nil {
		return err
	}
	if err := promoteClassCompute(spec, doc); err != nil {
		return err
	}
	spec.ConfigSpec, err = doc.Marshal()
	return err
}

// ProjectClassConfigSpec writes typed defaults into an older API's ConfigSpec.
func ProjectClassConfigSpec(spec *VirtualMachineClassSpec) (json.RawMessage, error) {
	doc, err := ParseClassConfigSpec(spec.ConfigSpec)
	if err != nil {
		return nil, err
	}
	if err := projectClassCompute(spec, doc); err != nil {
		return nil, err
	}
	return doc.Marshal()
}
