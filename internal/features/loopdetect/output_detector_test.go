package loopdetect

import (
	"testing"
)

func TestOutputDetector_NoRepeat_NoDetection(t *testing.T) {
	d := NewOutputDetector(6, 10, "enforce")
	inputs := []string{
		"Merhaba, nasılsın?",
		"Ben iyiyim, teşekkürler.",
		"Bugün hava çok güzel.",
	}
	for _, in := range inputs {
		r := d.Feed(in)
		if r.Detected {
			t.Fatalf("unexpected detection on normal input: %q", in)
		}
	}
}

func TestOutputDetector_ConsecutiveRepeat_Detects(t *testing.T) {
	d := NewOutputDetector(3, 10, "enforce")

	// Feed the same sentence 3 times — should detect on 3rd
	sentence := "Tabii, şimdi status=sold olan petleri sorguluyorum:"

	r1 := d.Feed(sentence)
	if r1.Detected {
		t.Fatal("detected on 1st occurrence")
	}

	r2 := d.Feed(sentence)
	if r2.Detected {
		t.Fatal("detected on 2nd occurrence (threshold=3)")
	}

	r3 := d.Feed(sentence)
	if !r3.Detected {
		t.Fatal("expected detection on 3rd consecutive repeat")
	}
	if r3.RepeatCount != 3 {
		t.Fatalf("expected repeatCount=3, got %d", r3.RepeatCount)
	}
	if r3.Sentence != sentence {
		t.Fatalf("expected sentence=%q, got %q", sentence, r3.Sentence)
	}
	if r3.Mode != "enforce" {
		t.Fatalf("expected mode=enforce, got %q", r3.Mode)
	}
}

func TestOutputDetector_InterruptedRepeat_Resets(t *testing.T) {
	d := NewOutputDetector(3, 10, "enforce")

	sentence := "Tabii, şimdi status=sold olan petleri sorguluyorum:"

	d.Feed(sentence) // count=1
	d.Feed(sentence) // count=2

	// Different sentence resets the counter
	r := d.Feed("Farklı bir cümle burada.")
	if r.Detected {
		t.Fatal("detected after different sentence")
	}
	if r.RepeatCount != 0 {
		t.Fatalf("expected repeatCount=0 after different sentence, got %d", r.RepeatCount)
	}

	// Same sentence again — counts from 1
	d.Feed(sentence)
	r = d.Feed(sentence)
	if r.Detected {
		t.Fatal("detected too early after reset")
	}
}

func TestOutputDetector_ObserveMode_DoesNotEnforce(t *testing.T) {
	d := NewOutputDetector(2, 10, "observe")

	sentence := "Tekrar eden cümle budur."

	r1 := d.Feed(sentence)
	if r1.Detected {
		t.Fatal("detected on 1st")
	}

	r2 := d.Feed(sentence)
	if !r2.Detected {
		t.Fatal("expected detection on 2nd repeat")
	}
	if r2.Mode != "observe" {
		t.Fatalf("expected mode=observe, got %q", r2.Mode)
	}
}

func TestOutputDetector_OffMode_NeverDetects(t *testing.T) {
	d := NewOutputDetector(2, 10, "off")

	sentence := "Same thing over and over. Same thing over and over."

	for i := 0; i < 10; i++ {
		r := d.Feed(sentence)
		if r.Detected {
			t.Fatal("detected while mode=off")
		}
	}
}

func TestOutputDetector_ShortSentence_Ignored(t *testing.T) {
	d := NewOutputDetector(3, 50, "enforce") // minSentenceLen=50

	// Short sentences should NOT trigger detection
	short := "Merhaba!"
	for i := 0; i < 5; i++ {
		r := d.Feed(short)
		if r.Detected {
			t.Fatal("short sentence should not trigger detection")
		}
	}
}

func TestOutputDetector_AccumulatedDeltas(t *testing.T) {
	// Simulate streaming deltas: the same sentence split across chunks
	d := NewOutputDetector(3, 10, "enforce")

	parts := []string{"Tabii, şimdi ", "status=sold olan ", "petleri sorguluyorum:"}

	// First occurrence
	for _, p := range parts {
		r := d.Feed(p)
		if r.Detected {
			t.Fatalf("unexpected detection during first occurrence on %q", p)
		}
	}

	// Second occurrence (after ":")
	for _, p := range parts {
		r := d.Feed(p)
		if r.Detected {
			t.Fatalf("unexpected detection during second occurrence on %q", p)
		}
	}

	// Third occurrence — last chunk should trigger detection
	// (since threshold=3, and the complete sentence "Tabii, şimdi status=sold olan petleri sorguluyorum:"
	//  has now appeared 3 times)
	for i, p := range parts {
		r := d.Feed(p)
		if i == len(parts)-1 && !r.Detected {
			t.Fatalf("expected detection on 3rd occurrence last chunk %q", p)
		}
	}
}

func TestOutputDetector_ColonBoundary(t *testing.T) {
	// Turkish sentences ending with ":" should be recognized as complete sentences
	d := NewOutputDetector(2, 10, "enforce")

	// First sentence ending with ":"
	r := d.Feed("Tabii, şimdi sorguluyorum:")
	if r.Detected {
		t.Fatal("unexpected detection on first")
	}

	// Second identical sentence — should detect with threshold=2
	r = d.Feed("Tabii, şimdi sorguluyorum:")
	if !r.Detected {
		t.Fatal("expected detection on second identical colon-ended sentence")
	}
}

func TestOutputDetector_SentenceEndChars(t *testing.T) {
	d := NewOutputDetector(2, 10, "enforce")

	tests := []struct {
		name  string
		sents []string
	}{
		{"dot", []string{"Bugün hava çok güzel. ", "Bugün hava çok güzel. "}},
		{"exclamation", []string{"Harika bir gün! ", "Harika bir gün! "}},
		{"question", []string{"Bugün nasılsın? ", "Bugün nasılsın? "}},
		{"newline", []string{"İlk satır.\n", "İlk satır.\n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d.Reset()
			r1 := d.Feed(tt.sents[0])
			if r1.Detected {
				t.Fatal("unexpected on first")
			}
			r2 := d.Feed(tt.sents[1])
			if !r2.Detected {
				t.Fatal("expected detection on second")
			}
		})
	}
}

func TestNewOutputDetectorDefaults(t *testing.T) {
	d := NewOutputDetector(0, 0, "")
	if d.threshold != 6 {
		t.Errorf("expected default threshold 6, got %d", d.threshold)
	}
	if d.minSentenceLen != 20 {
		t.Errorf("expected default minSentenceLen 20, got %d", d.minSentenceLen)
	}
	if d.mode != "observe" {
		t.Errorf("expected default mode observe, got %q", d.mode)
	}
}

func TestOutputDetector_EmptyDelta(t *testing.T) {
	d := NewOutputDetector(3, 10, "enforce")

	// Empty deltas should not affect state
	for i := 0; i < 5; i++ {
		r := d.Feed("")
		if r.Detected {
			t.Fatal("empty delta should not trigger detection")
		}
	}

	// Non-empty should still work after empty deltas
	r := d.Feed("Tekrar eden cumle. ")
	if r.Detected {
		t.Fatal("unexpected on first")
	}
}

func TestOutputDetector_Reset(t *testing.T) {
	d := NewOutputDetector(2, 10, "enforce")

	// Build up state
	s := "Test cümlesi. "
	d.Feed(s)
	d.Feed(s) // would detect

	d.Reset()

	// After reset, should start fresh
	r := d.Feed(s)
	if r.Detected {
		t.Fatal("detected after reset")
	}
}
