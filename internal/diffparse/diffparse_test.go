package diffparse

import "testing"

const sample = `diff --git a/internal/order/handler.go b/internal/order/handler.go
index 1111111..2222222 100644
--- a/internal/order/handler.go
+++ b/internal/order/handler.go
@@ -128,6 +128,8 @@ func (h *Handler) Create() {
 	if err != nil {
-		return nil, err
+		if errors.Is(err, sql.ErrNoRows) {
+			return nil, ErrNotFound
+		}
 	}

 	resp, err := h.store.Save(ctx)
diff --git a/internal/store/errors.go b/internal/store/errors.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/internal/store/errors.go
@@ -0,0 +1,2 @@
+package store
+var ErrNotFound = errors.New("nf")
\ No newline at end of file
diff --git a/old.txt b/old.txt
deleted file mode 100644
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/a.go b/b.go
similarity index 90%
rename from a.go
rename to b.go
diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
`

func TestParse(t *testing.T) {
	fs := Parse(sample)
	if len(fs) != 5 {
		t.Fatalf("files = %d, want 5", len(fs))
	}
	f := fs[0]
	if f.Path != "internal/order/handler.go" || f.Status != "M" || f.Add != 3 || f.Del != 1 {
		t.Fatalf("file0 = %+v", f)
	}
	ls := f.Hunks[0].Lines
	if ls[0].O != 128 || ls[0].N != 128 || ls[1].T != "-" || ls[1].O != 129 || ls[2].N != 129 {
		t.Fatalf("numbers wrong: %+v", ls[:3])
	}
	if ls[4].T != "+" || ls[4].N != 131 || ls[5].O != 130 || ls[5].N != 132 {
		t.Fatalf("numbers after adds wrong: %+v", ls[4:6])
	}
	if ls[6].T != " " || ls[6].Text != "" {
		t.Fatalf("empty context line: %+v", ls[6])
	}
	if fs[1].Status != "A" || fs[1].Path != "internal/store/errors.go" || fs[1].Hunks[0].Lines[0].N != 1 || fs[1].Add != 2 {
		t.Fatalf("added file = %+v", fs[1])
	}
	if fs[2].Status != "D" || fs[2].Path != "old.txt" || fs[2].Del != 1 {
		t.Fatalf("deleted file = %+v", fs[2])
	}
	if fs[3].Status != "R" || fs[3].Path != "b.go" || fs[3].OldPath != "a.go" {
		t.Fatalf("renamed file = %+v", fs[3])
	}
	if !fs[4].Binary || fs[4].Path != "img.png" {
		t.Fatalf("binary file = %+v", fs[4])
	}
}

func TestAddedFile(t *testing.T) {
	f := AddedFile("x.txt", "a\nb\n")
	if f.Add != 2 || f.Hunks[0].Lines[1].N != 2 || f.Status != "A" {
		t.Fatalf("%+v", f)
	}
	if !AddedFile("bin", "a\x00b").Binary {
		t.Fatal("binary not detected")
	}
}
