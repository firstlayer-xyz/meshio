package threemf

import (
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

func unitTri() geom.Geometry {
	return geom.Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
		Indices:  []uint32{0, 1, 2},
	}
}

func TestSlotResolution(t *testing.T) {
	o := &Object{Filament: 3, Parts: []Part{
		{Name: "explicit", Geometry: unitTri(), Filament: 7},
		{Name: "inherits", Geometry: unitTri()},
	}}
	if got := o.slot(o.Parts[0]); got != 7 {
		t.Errorf("explicit slot: got %d, want 7", got)
	}
	if got := o.slot(o.Parts[1]); got != 3 {
		t.Errorf("inherited slot: got %d, want 3", got)
	}

	bare := &Object{Parts: []Part{{Name: "a", Geometry: unitTri()}}}
	if got := bare.slot(bare.Parts[0]); got != 1 {
		t.Errorf("default slot: got %d, want 1", got)
	}
}

func TestSetSlotColorAllocatesNilMap(t *testing.T) {
	o := &Object{}
	o.SetSlotColor(3, "#C81E1E") // must not panic on the nil map
	if o.SlotColors[3] != "#C81E1E" {
		t.Errorf("SlotColors[3] = %q, want #C81E1E", o.SlotColors[3])
	}
}

func TestObjectValidate(t *testing.T) {
	tests := []struct {
		name string
		obj  *Object
		want string // substring of the expected error; "" means valid
	}{
		{
			name: "valid",
			obj:  &Object{Parts: []Part{{Name: "a", Geometry: unitTri()}}},
			want: "",
		},
		{
			name: "no parts",
			obj:  &Object{},
			want: "no parts",
		},
		{
			name: "empty geometry",
			obj:  &Object{Parts: []Part{{Name: "hollow"}}},
			want: "empty geometry",
		},
		{
			name: "negative part slot",
			obj:  &Object{Parts: []Part{{Name: "a", Geometry: unitTri(), Filament: -1}}},
			want: "negative filament slot",
		},
		{
			name: "negative object slot",
			obj:  &Object{Filament: -2, Parts: []Part{{Name: "a", Geometry: unitTri()}}},
			want: "negative filament slot",
		},
		{
			name: "duplicate attachment",
			obj: &Object{
				Parts: []Part{{Name: "a", Geometry: unitTri()}},
				Attachments: []geom.Attachment{
					{Path: "Metadata/x.json", ContentType: "application/json"},
					{Path: "Metadata/x.json", ContentType: "application/json"},
				},
			},
			want: "duplicate attachment",
		},
		{
			name: "attachment collides with root model part",
			obj: &Object{
				Parts:       []Part{{Name: "a", Geometry: unitTri()}},
				Attachments: []geom.Attachment{{Path: "3D/3dmodel.model", ContentType: "application/xml"}},
			},
			want: "reserved 3MF package part",
		},
		{
			name: "attachment collides with model_settings.config",
			obj: &Object{
				Parts:       []Part{{Name: "a", Geometry: unitTri()}},
				Attachments: []geom.Attachment{{Path: "Metadata/model_settings.config", ContentType: "application/xml"}},
			},
			want: "reserved 3MF package part",
		},
		{
			name: "attachment collides with the generated object part file",
			obj: &Object{
				// One part -> containerID = 2 -> 3D/Objects/object_2.model.
				Parts:       []Part{{Name: "a", Geometry: unitTri()}},
				Attachments: []geom.Attachment{{Path: "3D/Objects/object_2.model"}},
			},
			want: "reserved 3MF package part",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// validate takes the reserved set from its caller; these cases
			// cover the Bambu writer's, which includes the generated
			// objects part named after the container id.
			err := tc.obj.validate(tc.obj.bambuReservedPaths()...)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
