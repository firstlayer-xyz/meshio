package threemf

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
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

// xmlAttr escapes s for safe interpolation inside a double-quoted XML
// attribute value. Every writer that splices free-form data (Object.Name,
// Part.Name, Attachment.Path, Attachment.ContentType) into a hand-built XML
// attribute must pass it through here first -- an unescaped "&", "<", or
// '"' produces XML that fails to parse, silently losing whatever data lived
// in that attribute (e.g. every filament slot assignment in
// Metadata/model_settings.config).
func xmlAttr(s string) string {
	var buf bytes.Buffer
	// xml.EscapeText escapes '&', '<', '>', '"', and '\'', which covers both
	// attribute values and general text content.
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// EscapeText only fails if the writer fails; bytes.Buffer never does.
		panic(fmt.Sprintf("meshio: escaping xml attribute: %v", err))
	}
	return buf.String()
}

// validateAttachments rejects an attachment list that cannot produce a valid
// OPC package: one that repeats a Path, collides with a reserved package part
// name the writer itself is about to emit (e.g. "3D/3dmodel.model"), or omits
// a ContentType.
//
// archive/zip accepts duplicate part names silently, so without the path checks
// the package would contain two entries for the same name, and which one a
// reader resolves is reader-dependent -- invalid, but fine-looking in a zip
// browser. An empty ContentType is likewise unrepresentable: it would be
// written as ContentType="", which a validating reader rejects.
func validateAttachments(attachments []geom.Attachment, reserved ...string) error {
	isReserved := make(map[string]bool, len(reserved))
	for _, r := range reserved {
		isReserved[r] = true
	}
	seen := make(map[string]bool, len(attachments))
	for _, a := range attachments {
		if seen[a.Path] {
			return fmt.Errorf("meshio: duplicate attachment path %q", a.Path)
		}
		seen[a.Path] = true
		if isReserved[a.Path] {
			return fmt.Errorf("meshio: attachment path %q collides with a reserved 3MF package part", a.Path)
		}
		if a.ContentType == "" {
			return fmt.Errorf("meshio: attachment %q has no ContentType", a.Path)
		}
	}
	return nil
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

// contentTypes is the type declarations from an OPC [Content_Types].xml.
//
// OPC declares a part's type two ways: <Default Extension> covers every part
// with that extension, and <Override PartName> names a single part. Reading
// only Overrides loses the type of everything declared by extension, which in a
// real package means the png thumbnails and gcode.
type contentTypes struct {
	byPart map[string]string // absolute part name ("/Metadata/x.json") -> type
	byExt  map[string]string // lowercased extension without dot -> type
}

// parseContentTypes reads the <Default> and <Override> declarations.
func parseContentTypes(xmlText string) contentTypes {
	ct := contentTypes{byPart: map[string]string{}, byExt: map[string]string{}}
	dec := xml.NewDecoder(strings.NewReader(xmlText))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var part, ext, typ string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "PartName":
				part = a.Value
			case "Extension":
				ext = a.Value
			case "ContentType":
				typ = a.Value
			}
		}
		switch se.Name.Local {
		case "Override":
			if part != "" {
				ct.byPart[part] = typ
			}
		case "Default":
			if ext != "" {
				ct.byExt[strings.ToLower(ext)] = typ
			}
		}
	}
	return ct
}

// of returns the declared content type for a package-relative part name, or ""
// if the package declares none. An Override naming the part wins over a Default
// for its extension, per OPC.
func (c contentTypes) of(name string) string {
	if typ, ok := c.byPart["/"+name]; ok {
		return typ
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		return c.byExt[strings.ToLower(name[i+1:])]
	}
	return ""
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
