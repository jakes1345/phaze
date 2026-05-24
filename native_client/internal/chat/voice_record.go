package chat

// Voice message recording via PulseAudio.
//
// Usage:
//   StartVoiceRecord()
//   // ... wait for user to stop ...
//   filePath, err := StopVoiceRecord()
//   // filePath is a WAV file; caller is responsible for deleting it.
//
// Only one recording session may be active at a time (enforced by recMu).

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"
)

const (
	recSampleRate = 16000          // 16 kHz for voice clarity
	recChannels   = 1              // mono
	recMaxSeconds = 120            // 2 minutes hard cap
)

var (
	recMu      sync.Mutex
	recActive  bool
	recStopCh  chan struct{}
	recDoneCh  chan recResult
)

type recResult struct {
	path string
	err  error
}

// StartVoiceRecord begins recording from the default PulseAudio source.
// It is a no-op if a recording is already in progress.
func StartVoiceRecord() {
	recMu.Lock()
	defer recMu.Unlock()
	if recActive {
		return
	}
	recActive = true
	recStopCh = make(chan struct{})
	recDoneCh = make(chan recResult, 1)
	go runRecording(recStopCh, recDoneCh)
}

// StopVoiceRecord stops the active recording and returns the path to the
// written WAV file. The caller must delete the file when done.
func StopVoiceRecord() (string, error) {
	recMu.Lock()
	if !recActive {
		recMu.Unlock()
		return "", fmt.Errorf("no active recording")
	}
	stopCh := recStopCh
	doneCh := recDoneCh
	recMu.Unlock()

	close(stopCh)
	res := <-doneCh

	recMu.Lock()
	recActive = false
	recMu.Unlock()

	return res.path, res.err
}

func runRecording(stopCh <-chan struct{}, done chan<- recResult) {
	client, err := getPulse()
	if err != nil {
		done <- recResult{err: fmt.Errorf("pulse unavailable: %w", err)}
		return
	}

	sampleCh := make(chan int16, recSampleRate*recMaxSeconds)

	stream, err := client.NewRecord(
		pulse.Int16Writer(func(buf []int16) (int, error) {
			for _, s := range buf {
				select {
				case sampleCh <- s:
				default:
					// buffer full (hit 2 min cap) — drop
				}
			}
			return len(buf), nil
		}),
		pulse.RecordSampleRate(recSampleRate),
		pulse.RecordMono,
	)
	if err != nil {
		done <- recResult{err: fmt.Errorf("open mic: %w", err)}
		return
	}
	stream.Start()

	// Stop either on signal or hard cap
	timeout := time.NewTimer(recMaxSeconds * time.Second)
	defer timeout.Stop()
	select {
	case <-stopCh:
	case <-timeout.C:
	}
	stream.Stop()
	stream.Close()

	// Drain remaining samples
	close(sampleCh)
	var samples []int16
	for s := range sampleCh {
		samples = append(samples, s)
	}

	// Write WAV
	f, err := os.CreateTemp("", "phaze-voice-*.wav")
	if err != nil {
		done <- recResult{err: fmt.Errorf("temp file: %w", err)}
		return
	}
	if err := writeWAV(f, samples, recSampleRate); err != nil {
		f.Close()
		os.Remove(f.Name())
		done <- recResult{err: fmt.Errorf("write wav: %w", err)}
		return
	}
	f.Close()
	done <- recResult{path: f.Name()}
}

// writeWAV writes a minimal PCM WAV file for 16-bit mono samples.
func writeWAV(f *os.File, samples []int16, sampleRate int) error {
	numSamples := len(samples)
	dataSize := numSamples * 2 // 16-bit = 2 bytes/sample
	fileSize := 36 + dataSize

	write := func(b []byte) error {
		_, err := f.Write(b)
		return err
	}
	writeU32LE := func(v uint32) error {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		return write(b[:])
	}
	writeU16LE := func(v uint16) error {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], v)
		return write(b[:])
	}

	// RIFF header
	if err := write([]byte("RIFF")); err != nil { return err }
	if err := writeU32LE(uint32(fileSize)); err != nil { return err }
	if err := write([]byte("WAVE")); err != nil { return err }

	// fmt chunk
	if err := write([]byte("fmt ")); err != nil { return err }
	if err := writeU32LE(16); err != nil { return err }    // chunk size
	if err := writeU16LE(1); err != nil { return err }     // PCM
	if err := writeU16LE(1); err != nil { return err }     // mono
	if err := writeU32LE(uint32(sampleRate)); err != nil { return err }
	if err := writeU32LE(uint32(sampleRate * 2)); err != nil { return err } // byte rate
	if err := writeU16LE(2); err != nil { return err }     // block align
	if err := writeU16LE(16); err != nil { return err }    // bits per sample

	// data chunk
	if err := write([]byte("data")); err != nil { return err }
	if err := writeU32LE(uint32(dataSize)); err != nil { return err }
	for _, s := range samples {
		if err := writeU16LE(uint16(s)); err != nil { return err }
	}
	return nil
}
