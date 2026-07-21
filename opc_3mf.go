package meshio

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// OPC (Open Packaging Conventions) plumbing shared by the 3MF reader and all
// 3MF writers: zip part I/O, [Content_Types].xml, and .rels relationships.

func toReaderAt(r io.Reader) (io.ReaderAt, int64, error) {
	if ra, ok := r.(interface {
		io.ReaderAt
		Stat() (os.FileInfo, error)
	}); ok {
		info, err := ra.Stat()
		if err != nil {
			return nil, 0, err
		}
		return ra, info.Size(), nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

func addZipEntry(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("meshio: creating %s: %w", name, err)
	}
	_, err = w.Write([]byte(content))
	if err != nil {
		return fmt.Errorf("meshio: writing %s: %w", name, err)
	}
	return nil
}

func addZipBytes(zw *zip.Writer, name string, content []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("meshio: creating %s: %w", name, err)
	}
	if _, err := w.Write(content); err != nil {
		return fmt.Errorf("meshio: writing %s: %w", name, err)
	}
	return nil
}

// parseContentTypeOverrides extracts PartName -> ContentType from the OPC
// [Content_Types].xml <Override> elements.
func parseContentTypeOverrides(xmlText string) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(xmlText))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Override" {
			continue
		}
		var part, ct string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "PartName":
				part = a.Value
			case "ContentType":
				ct = a.Value
			}
		}
		if part != "" {
			out[part] = ct
		}
	}
	return out
}

// rootModelFromRels returns the package-relative path of the 3MF root model part
// (the Target of the Relationship whose Type is the 3dmodel relationship), or ""
// if none is present. The leading "/" is stripped to match zip entry names.
func rootModelFromRels(relsXML string) string {
	dec := xml.NewDecoder(strings.NewReader(relsXML))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var target, typ string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Target":
				target = a.Value
			case "Type":
				typ = a.Value
			}
		}
		if strings.HasSuffix(typ, "/3dmodel") {
			return strings.TrimPrefix(target, "/")
		}
	}
}

// findRootModelPart returns the root model part name. It prefers the OPC
// _rels/.rels 3dmodel relationship; falls back to "3D/3dmodel.model", then to
// the sole .model part if there is exactly one.
func findRootModelPart(zr *zip.Reader) string {
	for _, f := range zr.File {
		if f.Name == "_rels/.rels" {
			if rc, err := f.Open(); err == nil {
				b, _ := io.ReadAll(rc)
				rc.Close()
				if root := rootModelFromRels(string(b)); root != "" {
					return root
				}
			}
		}
	}
	var models []string
	for _, f := range zr.File {
		if f.Name == "3D/3dmodel.model" {
			return f.Name
		}
		if strings.HasSuffix(f.Name, ".model") {
			models = append(models, f.Name)
		}
	}
	if len(models) == 1 {
		return models[0]
	}
	return ""
}
