package bluetooth

import (
	"encoding/binary"
	"errors"
	"math"
)

var (
	sbcRates     = [4]int{16000, 32000, 44100, 48000}
	sbcBlocks    = [4]int{4, 8, 12, 16}
	sbcOffset4   = [4][4]int{{-1, 0, 0, 0}, {-2, 0, 0, 1}, {-2, 0, 0, 1}, {-2, 0, 0, 1}}
	sbcOffset8   = [4][8]int{{-2, 0, 0, 0, 0, 0, 0, 1}, {-3, 0, 0, 0, 0, 0, 1, 2}, {-4, 0, 0, 0, 0, 0, 1, 2}, {-4, 0, 0, 0, 0, 0, 1, 2}}
	sbcSyncword  = byte(0x9C)
	errSBCSync   = errors.New("sbc: missing syncword")
	errSBCShort  = errors.New("sbc: truncated frame")
	errSBCCRC    = errors.New("sbc: CRC mismatch")
	errSBCConfig = errors.New("sbc: invalid bitpool")
)

// Standard Bluetooth A2DP SBC synthesis prototype window coefficients (Table B.1 & B.2).
var sbcProto4 = [40]float64{
	0.00000000e+00, 5.36548976e-04, 1.49188357e-03, 2.73370904e-03,
	3.83720193e-03, 3.89205149e-03, 1.86581691e-03, -3.06012286e-03,
	1.09137620e-02, 2.04385087e-02, 2.88757392e-02, 3.21939290e-02,
	2.58767811e-02, 6.13245186e-03, -2.88217274e-02, -7.76463494e-02,
	1.35593274e-01, 1.94987841e-01, 2.46636662e-01, 2.81828203e-01,
	2.94315332e-01, 2.81828203e-01, 2.46636662e-01, 1.94987841e-01,
	-1.35593274e-01, -7.76463494e-02, -2.88217274e-02, 6.13245186e-03,
	2.58767811e-02, 3.21939290e-02, 2.88757392e-02, 2.04385087e-02,
	-1.09137620e-02, -3.06012286e-03, 1.86581691e-03, 3.89205149e-03,
	3.83720193e-03, 2.73370904e-03, 1.49188357e-03, 5.36548976e-04,
}

var sbcProto8 = [80]float64{
	0.00000000e+00, 1.56575398e-04, 3.43256425e-04, 5.54620202e-04,
	8.23919506e-04, 1.13992507e-03, 1.47640169e-03, 1.78371725e-03,
	2.01182542e-03, 2.10371989e-03, 1.99454554e-03, 1.61656283e-03,
	9.02154502e-04, -1.78805361e-04, -1.64973098e-03, -3.49717454e-03,
	5.65949473e-03, 8.02941163e-03, 1.04584443e-02, 1.27472335e-02,
	1.46525263e-02, 1.59045603e-02, 1.62208471e-02, 1.53184106e-02,
	1.29371806e-02, 8.85757540e-03, 2.92408442e-03, -4.91578024e-03,
	-1.46404076e-02, -2.61098752e-02, -3.90751381e-02, -5.31873032e-02,
	6.79989431e-02, 8.29847578e-02, 9.75753918e-02, 1.11196689e-01,
	1.23264548e-01, 1.33264415e-01, 1.40753505e-01, 1.45389847e-01,
	1.46955068e-01, 1.45389847e-01, 1.40753505e-01, 1.33264415e-01,
	1.23264548e-01, 1.11196689e-01, 9.75753918e-02, 8.29847578e-02,
	-6.79989431e-02, -5.31873032e-02, -3.90751381e-02, -2.61098752e-02,
	-1.46404076e-02, -4.91578024e-03, 2.92408442e-03, 8.85757540e-03,
	1.29371806e-02, 1.53184106e-02, 1.62208471e-02, 1.59045603e-02,
	1.46525263e-02, 1.27472335e-02, 1.04584443e-02, 8.02941163e-03,
	-5.65949473e-03, -3.49717454e-03, -1.64973098e-03, -1.78805361e-04,
	9.02154502e-04, 1.61656283e-03, 1.99454554e-03, 2.10371989e-03,
	2.01182542e-03, 1.78371725e-03, 1.47640169e-03, 1.13992507e-03,
	8.23919506e-04, 5.54620202e-04, 3.43256425e-04, 1.56575398e-04,
}

// SBCDecoder decodes A2DP SBC frames into interleaved S16_LE PCM.
type SBCDecoder struct {
	v [2][160]float64
}

type SBCFrameInfo struct {
	SampleRate int
	Channels   int
	Blocks     int
	Subbands   int
	FrameBytes int
}

// DecodeRTP decodes one A2DP RTP/SBC packet into S16_LE PCM bytes.
func (d *SBCDecoder) DecodeRTP(pkt []byte) ([]byte, SBCFrameInfo, error) {
	if len(pkt) < 13 {
		return nil, SBCFrameInfo{}, errSBCShort
	}
	if (pkt[0] >> 6) != 2 {
		return nil, SBCFrameInfo{}, errSBCSync
	}
	cc := int(pkt[0] & 0x0F)
	off := 12 + cc*4
	if (pkt[0] & 0x10) != 0 {
		if off+4 > len(pkt) {
			return nil, SBCFrameInfo{}, errSBCShort
		}
		extWords := int(binary.BigEndian.Uint16(pkt[off+2 : off+4]))
		off += 4 + extWords*4
	}
	if off >= len(pkt) {
		return nil, SBCFrameInfo{}, errSBCShort
	}
	end := len(pkt)
	if (pkt[0] & 0x20) != 0 {
		pad := int(pkt[len(pkt)-1])
		if pad > 0 && end-pad > off {
			end -= pad
		}
	}
	// A2DP SBC media payload header: lower 4 bits hold frame count.
	frameCount := int(pkt[off] & 0x0F)
	off++
	if frameCount == 0 {
		frameCount = 1
	}

	var pcm []byte
	var lastInfo SBCFrameInfo
	for i := 0; i < frameCount && off < end; i++ {
		framePCM, info, err := d.DecodeFrame(pkt[off:end])
		if err != nil {
			if len(pcm) > 0 {
				return pcm, lastInfo, nil
			}
			return nil, SBCFrameInfo{}, err
		}
		pcm = append(pcm, framePCM...)
		lastInfo = info
		off += info.FrameBytes
	}
	return pcm, lastInfo, nil
}

// DecodeFrame decodes a single SBC frame into S16_LE PCM bytes.
func (d *SBCDecoder) DecodeFrame(frame []byte) ([]byte, SBCFrameInfo, error) {
	if len(frame) < 4 {
		return nil, SBCFrameInfo{}, errSBCShort
	}
	if frame[0] != sbcSyncword {
		return nil, SBCFrameInfo{}, errSBCSync
	}
	freqIdx := int((frame[1] >> 6) & 0x03)
	blocksIdx := int((frame[1] >> 4) & 0x03)
	chMode := int((frame[1] >> 2) & 0x03)
	loudness := ((frame[1] >> 1) & 0x01) == 0
	subbands := 4
	if (frame[1] & 0x01) != 0 {
		subbands = 8
	}
	blocks := sbcBlocks[blocksIdx]
	sampleRate := sbcRates[freqIdx]
	channels := 2
	if chMode == 0 {
		channels = 1
	}
	bitpool := int(frame[2])
	if bitpool < 2 {
		return nil, SBCFrameInfo{}, errSBCConfig
	}

	frameBytes := sbcFrameLength(blocks, channels, subbands, chMode, bitpool)
	if len(frame) < frameBytes {
		return nil, SBCFrameInfo{}, errSBCShort
	}

	// Verify CRC-8 over header bytes [1..2] + join bits + scale factors.
	crcBits := 16 + channels*subbands*4
	if chMode == 3 {
		crcBits += subbands
	}
	if got := sbcCRC8(frame[1:], crcBits); got != frame[3] {
		return nil, SBCFrameInfo{}, errSBCCRC
	}

	br := bitReader{data: frame, bitPos: 32}
	var join [8]bool
	if chMode == 3 {
		for sb := 0; sb < subbands; sb++ {
			join[sb] = br.readBits(1) != 0
		}
		join[subbands-1] = false
	}

	var sf [2][8]int
	for ch := 0; ch < channels; ch++ {
		for sb := 0; sb < subbands; sb++ {
			sf[ch][sb] = int(br.readBits(4))
		}
	}

	var bits [2][8]int
	sbcCalculateBits(freqIdx, chMode, subbands, bitpool, loudness, &sf, &bits)

	var sbSamples [16][2][8]float64
	for blk := 0; blk < blocks; blk++ {
		for ch := 0; ch < channels; ch++ {
			for sb := 0; sb < subbands; sb++ {
				b := bits[ch][sb]
				if b == 0 {
					sbSamples[blk][ch][sb] = 0
					continue
				}
				raw := br.readBits(b)
				levels := float64((uint32(1) << b) - 1)
				scale := float64(uint32(1) << (sf[ch][sb] + 1))
				sbSamples[blk][ch][sb] = (((float64(raw)*2.0 + 1.0) / levels) - 1.0) * scale
			}
		}
		if chMode == 3 {
			for sb := 0; sb < subbands; sb++ {
				if join[sb] {
					m := sbSamples[blk][0][sb]
					s := sbSamples[blk][1][sb]
					sbSamples[blk][0][sb] = m + s
					sbSamples[blk][1][sb] = m - s
				}
			}
		}
	}

	totalSamples := blocks * subbands * channels
	pcm := make([]byte, totalSamples*2)
	pcmIdx := 0
	var outBlock [8]float64
	for blk := 0; blk < blocks; blk++ {
		var chOut [2][8]int16
		for ch := 0; ch < channels; ch++ {
			if subbands == 4 {
				synthesize4(&d.v[ch], sbSamples[blk][ch][:4], outBlock[:4])
			} else {
				synthesize8(&d.v[ch], sbSamples[blk][ch][:8], outBlock[:8])
			}
			for sb := 0; sb < subbands; sb++ {
				chOut[ch][sb] = clampPCM16(outBlock[sb])
			}
		}
		for sb := 0; sb < subbands; sb++ {
			for ch := 0; ch < channels; ch++ {
				binary.LittleEndian.PutUint16(pcm[pcmIdx:pcmIdx+2], uint16(chOut[ch][sb]))
				pcmIdx += 2
			}
		}
	}

	return pcm, SBCFrameInfo{
		SampleRate: sampleRate,
		Channels:   channels,
		Blocks:     blocks,
		Subbands:   subbands,
		FrameBytes: frameBytes,
	}, nil
}

func sbcFrameLength(blocks, channels, subbands, chMode, bitpool int) int {
	bits := 32 + 4*subbands*channels
	switch chMode {
	case 0, 1: // Mono / Dual Channel
		bits += blocks * channels * bitpool
	case 2: // Stereo
		bits += blocks * bitpool
	case 3: // Joint Stereo
		bits += subbands + blocks*bitpool
	}
	return (bits + 7) / 8
}

func sbcCalculateBits(freqIdx, chMode, subbands, bitpool int, loudness bool, sf *[2][8]int, bits *[2][8]int) {
	channels := 2
	if chMode == 0 {
		channels = 1
	}

	computeChannel := func(chStart, chEnd, pool int) {
		var bitneed [2][8]int
		maxBitneed := 0
		for ch := chStart; ch < chEnd; ch++ {
			for sb := 0; sb < subbands; sb++ {
				if sf[ch][sb] == 0 {
					bitneed[ch][sb] = -5
				} else if !loudness {
					bitneed[ch][sb] = sf[ch][sb]
				} else {
					var off int
					if subbands == 4 {
						off = sbcOffset4[freqIdx][sb]
					} else {
						off = sbcOffset8[freqIdx][sb]
					}
					l := sf[ch][sb] - off
					if l > 0 {
						bitneed[ch][sb] = l / 2
					} else {
						bitneed[ch][sb] = l
					}
				}
				if bitneed[ch][sb] > maxBitneed {
					maxBitneed = bitneed[ch][sb]
				}
			}
		}

		bitcount := 0
		slicecount := 0
		bitslice := maxBitneed + 1
		for {
			bitslice--
			bitcount += slicecount
			slicecount = 0
			for ch := chStart; ch < chEnd; ch++ {
				for sb := 0; sb < subbands; sb++ {
					if bitneed[ch][sb] > bitslice+1 && bitneed[ch][sb] < bitslice+16 {
						slicecount++
					} else if bitneed[ch][sb] == bitslice+1 {
						slicecount += 2
					}
				}
			}
			if bitcount+slicecount >= pool {
				break
			}
		}
		if bitcount+slicecount == pool {
			bitcount += slicecount
			bitslice--
		}
		for ch := chStart; ch < chEnd; ch++ {
			for sb := 0; sb < subbands; sb++ {
				if bitneed[ch][sb] < bitslice+2 {
					bits[ch][sb] = 0
				} else {
					b := bitneed[ch][sb] - bitslice
					if b > 16 {
						b = 16
					}
					bits[ch][sb] = b
				}
			}
		}

		sb := 0
		ch := chStart
		for bitcount < pool && sb < subbands {
			if bits[ch][sb] >= 2 && bits[ch][sb] < 16 {
				bits[ch][sb]++
				bitcount++
			} else if bitneed[ch][sb] == bitslice+1 && pool > bitcount+1 {
				bits[ch][sb] = 2
				bitcount += 2
			}
			if chEnd-chStart > 1 && ch == chStart {
				ch = chStart + 1
			} else {
				ch = chStart
				sb++
			}
		}
		sb = 0
		ch = chStart
		for bitcount < pool && sb < subbands {
			if bits[ch][sb] < 16 {
				bits[ch][sb]++
				bitcount++
			}
			if chEnd-chStart > 1 && ch == chStart {
				ch = chStart + 1
			} else {
				ch = chStart
				sb++
			}
		}
	}

	if chMode == 0 || chMode == 1 {
		for ch := 0; ch < channels; ch++ {
			computeChannel(ch, ch+1, bitpool)
		}
	} else {
		computeChannel(0, 2, bitpool)
	}
}

func synthesize4(v *[160]float64, sb []float64, out []float64) {
	copy(v[8:], v[:72])
	for i := 0; i < 8; i++ {
		var sum float64
		for k := 0; k < 4; k++ {
			sum += sb[k] * math.Cos(float64(i+2)*float64(2*k+1)*math.Pi/8.0)
		}
		v[i] = sum
	}
	var u [40]float64
	for i := 0; i < 5; i++ {
		for j := 0; j < 4; j++ {
			u[i*8+j] = v[i*16+j]
			u[i*8+4+j] = v[i*16+12+j]
		}
	}
	var w [40]float64
	for i := 0; i < 40; i++ {
		w[i] = u[i] * sbcProto4[i] * -4.0
	}
	for j := 0; j < 4; j++ {
		var s float64
		for i := 0; i < 10; i++ {
			s += w[j+i*4]
		}
		out[j] = s
	}
}

func synthesize8(v *[160]float64, sb []float64, out []float64) {
	copy(v[16:], v[:144])
	for i := 0; i < 16; i++ {
		var sum float64
		for k := 0; k < 8; k++ {
			sum += sb[k] * math.Cos(float64(i+4)*float64(2*k+1)*math.Pi/16.0)
		}
		v[i] = sum
	}
	var u [80]float64
	for i := 0; i < 5; i++ {
		for j := 0; j < 8; j++ {
			u[i*16+j] = v[i*32+j]
			u[i*16+8+j] = v[i*32+24+j]
		}
	}
	var w [80]float64
	for i := 0; i < 80; i++ {
		w[i] = u[i] * sbcProto8[i] * -8.0
	}
	for j := 0; j < 8; j++ {
		var s float64
		for i := 0; i < 10; i++ {
			s += w[j+i*8]
		}
		out[j] = s
	}
}

func clampPCM16(v float64) int16 {
	// SBC subband samples are already in signed 16-bit PCM units.
	s := math.Round(v)
	if s > 32767.0 {
		return 32767
	}
	if s < -32768.0 {
		return -32768
	}
	return int16(s)
}

func sbcCRC8(hdrFromByte1 []byte, totalBits int) byte {
	// CRC-8 over: 2 header bytes (skipping CRC byte at index 2), then remaining bits starting at index 3.
	crc := byte(0x0F)
	feedBit := func(bit byte) {
		xor := ((crc >> 7) ^ bit) & 1
		crc <<= 1
		if xor != 0 {
			crc ^= 0x1D
		}
	}
	for i := 0; i < 16 && i < totalBits; i++ {
		b := hdrFromByte1[i/8]
		feedBit((b >> (7 - (i % 8))) & 1)
	}
	rem := totalBits - 16
	for i := 0; i < rem; i++ {
		idx := 3 + i/8
		if idx >= len(hdrFromByte1) {
			break
		}
		b := hdrFromByte1[idx]
		feedBit((b >> (7 - (i % 8))) & 1)
	}
	return crc
}

type bitReader struct {
	data   []byte
	bitPos int
}

func (r *bitReader) readBits(n int) uint32 {
	var val uint32
	for i := 0; i < n; i++ {
		byteIdx := r.bitPos / 8
		bitIdx := 7 - (r.bitPos % 8)
		r.bitPos++
		var bit uint32
		if byteIdx < len(r.data) {
			bit = uint32((r.data[byteIdx] >> bitIdx) & 1)
		}
		val = (val << 1) | bit
	}
	return val
}
