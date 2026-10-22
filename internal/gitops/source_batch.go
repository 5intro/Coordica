package gitops

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"

	"coordica/internal/process"
)

type sourceBlob struct {
	path, oid string
	size      int
}

func sourceManifest(tree string, include func(string) bool) ([]sourceBlob, error) {
	entries := strings.Split(strings.TrimSuffix(tree, "\x00"), "\x00")
	if len(entries) > 20000 {
		return nil, &SourceInventoryError{Cause: ErrOutputLimit}
	}
	var blobs []sourceBlob
	total := 0
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		header, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 4 || ValidatePath(path) != nil || path == "." {
			return nil, &SourceInventoryError{Cause: ErrUnsupportedObject}
		}
		if !include(path) {
			continue
		}
		if fields[0] != "100644" && fields[0] != "100755" || fields[1] != "blob" || validIDs(OID(fields[2])) != nil {
			return nil, &SourceInventoryError{Cause: ErrUnsupportedObject}
		}
		size, err := strconv.Atoi(fields[3])
		if err != nil || size < 0 || size > 2<<20 || total+size > 64<<20 {
			return nil, &SourceInventoryError{Cause: ErrOutputLimit}
		}
		total += size
		blobs = append(blobs, sourceBlob{path: path, oid: fields[2], size: size})
	}
	return blobs, nil
}

func (r SourceReader) readSourceBatch(ctx context.Context, repo string, blobs []sourceBlob, consume func(string, string) error) error {
	if len(blobs) == 0 {
		return nil
	}
	var input strings.Builder
	for _, blob := range blobs {
		input.WriteString(blob.oid + "\n")
	}
	spec, err := gitCommandSpec(r.Git, repo, nil, "cat-file", "--batch")
	if err != nil {
		return err
	}
	// 64 MiB content plus bounded headers for at most 20,000 full object IDs.
	out, stderr, result, err := process.CollectRawInput(ctx, spec, []byte(input.String()), 66<<20, 64<<10)
	if err = gitCommandFailure(ctx, "cat-file", stderr, result, err); err != nil {
		return err
	}
	return consumeSourceBatch(ctx, out, blobs, consume)
}

func consumeSourceBatch(ctx context.Context, out []byte, blobs []sourceBlob, consume func(string, string) error) error {
	for _, blob := range blobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		newline := bytes.IndexByte(out, '\n')
		if newline < 0 || newline > 128 {
			return errors.New("invalid immutable blob batch header")
		}
		header := strings.Fields(string(out[:newline]))
		if len(header) != 3 || header[0] != blob.oid || header[1] != "blob" || header[2] != strconv.Itoa(blob.size) {
			return errors.New("immutable blob batch identity or size mismatch")
		}
		out = out[newline+1:]
		if len(out) <= blob.size || out[blob.size] != '\n' {
			return errors.New("incomplete immutable blob batch")
		}
		if err := consume(blob.path, string(out[:blob.size])); err != nil {
			return errors.Join(errors.New("immutable source consumer rejected "+blob.path), err)
		}
		out = out[blob.size+1:]
	}
	if len(out) != 0 {
		return errors.New("unexpected trailing immutable blob batch data")
	}
	return nil
}
