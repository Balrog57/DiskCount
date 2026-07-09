package parsing

import (
	"testing"

	"github.com/Balrog57/DiskCount/internal/domain"
)

func TestNormalizeRecordingMethod(t *testing.T) {
	rot := domain.MediaTypeRotational
	ssd := domain.MediaTypeSolidState

	cases := []struct {
		name     string
		text     string
		media    *domain.MediaType
		want     domain.RecordingMethod
		wantNil  bool
	}{
		// Explicit CMR/SMR declarations win.
		{"explicit cmr", "Seagate 16TB CMR", &rot, domain.RecordingMethodCMR, false},
		{"explicit conventional", "WD conventional magnetic", &rot, domain.RecordingMethodCMR, false},
		{"explicit smr", "Seagate Archive SMR 8TB", &rot, domain.RecordingMethodSMR, false},
		{"explicit shingled", "drive shingled recording", &rot, domain.RecordingMethodSMR, false},

		// Known CMR families.
		{"exos", "Seagate Exos X20 18TB", &rot, domain.RecordingMethodCMR, false},
		{"ironwolf", "Seagate IronWolf Pro 16TB", &rot, domain.RecordingMethodCMR, false},
		{"wd red plus", "WD Red Plus 12TB", &rot, domain.RecordingMethodCMR, false},
		{"ultrastar", "WD Ultrastar DC HC550", &rot, domain.RecordingMethodCMR, false},
		{"toshiba mg", "Toshiba MG08 16TB", &rot, domain.RecordingMethodCMR, false},

		// Known SMR families.
		{"wd red base", "WD Red 4TB NAS", &rot, domain.RecordingMethodSMR, false},
		{"barracuda", "Seagate Barracuda 8TB", &rot, domain.RecordingMethodSMR, false},
		{"archive", "Seagate Archive HDD 6TB", &rot, domain.RecordingMethodSMR, false},

		// Unknown HDD model.
		{"unknown hdd", "Generic HDD 4TB", &rot, "", true},

		// SSDs always return nil.
		{"ssd cmr label ignored", "Samsung 980 SSD CMR", &ssd, "", true},
		{"ssd nil media nil", "Samsung 980", nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeRecordingMethod(tc.text, tc.media)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %s", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %s, got nil", tc.want)
			}
			if *got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, *got)
			}
		})
	}
}
