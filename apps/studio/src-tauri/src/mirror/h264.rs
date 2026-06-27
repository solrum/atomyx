//! H.264 Annex-B NAL unit splitter.
//!
//! Scans a growing byte stream for Annex-B start codes (0x00 0x00
//! 0x00 0x01 or 0x00 0x00 0x01) and yields each NAL unit along
//! with its type bits. Keyframes are identified by nal_unit_type
//! 5 (IDR slice); an SPS/PPS+IDR triplet in sequence also counts
//! as keyframe-aligned for HLS/fmp4 framing.
//!
//! Timestamps are not derived from the bitstream — AUD / slice
//! headers carry no wall-clock — so the caller attaches a
//! monotonic microsecond counter when pushing to JS.

#[derive(Debug, Clone)]
pub struct NalUnit {
    pub bytes: Vec<u8>,
    pub nal_type: u8,
    pub is_keyframe: bool,
}

pub struct NalSplitter {
    buffer: Vec<u8>,
}

impl NalSplitter {
    pub fn new() -> Self {
        Self {
            buffer: Vec::with_capacity(64 * 1024),
        }
    }

    /// Push bytes into the splitter and return every complete NAL
    /// that emerged. Partial NAL data remains buffered.
    pub fn push(&mut self, chunk: &[u8]) -> Vec<NalUnit> {
        self.buffer.extend_from_slice(chunk);

        let mut out = Vec::new();
        // Walk the buffer looking for start codes. Any NAL fully
        // contained between two start codes can be emitted; the
        // trailing partial (from the last start code to end-of-
        // buffer) stays until the next push.
        let start_offsets = find_start_codes(&self.buffer);
        if start_offsets.len() < 2 {
            return out;
        }

        for window in start_offsets.windows(2) {
            let (start, end) = (window[0], window[1]);
            // Skip the start code itself when extracting NAL body.
            let sc_len = start_code_len(&self.buffer, start);
            let body_start = start + sc_len;
            if body_start >= end {
                continue;
            }
            let nal = self.buffer[body_start..end].to_vec();
            if nal.is_empty() {
                continue;
            }
            out.push(classify_nal(nal));
        }

        // Retain from the last start code onward so the next chunk
        // can complete it.
        let keep_from = *start_offsets.last().unwrap();
        self.buffer.drain(..keep_from);
        out
    }

    /// Flush any remaining buffered bytes as a final NAL unit.
    /// Called once when the source pipe closes.
    pub fn flush(&mut self) -> Option<NalUnit> {
        if self.buffer.is_empty() {
            return None;
        }
        let starts = find_start_codes(&self.buffer);
        if let Some(&start) = starts.first() {
            let sc_len = start_code_len(&self.buffer, start);
            let body_start = start + sc_len;
            if body_start < self.buffer.len() {
                let nal = self.buffer[body_start..].to_vec();
                self.buffer.clear();
                if !nal.is_empty() {
                    return Some(classify_nal(nal));
                }
            }
        }
        self.buffer.clear();
        None
    }
}

impl Default for NalSplitter {
    fn default() -> Self {
        Self::new()
    }
}

fn find_start_codes(buf: &[u8]) -> Vec<usize> {
    let mut positions = Vec::new();
    let mut i = 0;
    while i + 3 <= buf.len() {
        if buf[i] == 0 && buf[i + 1] == 0 {
            if i + 4 <= buf.len() && buf[i + 2] == 0 && buf[i + 3] == 1 {
                positions.push(i);
                i += 4;
                continue;
            }
            if buf[i + 2] == 1 {
                positions.push(i);
                i += 3;
                continue;
            }
        }
        i += 1;
    }
    positions
}

fn start_code_len(buf: &[u8], offset: usize) -> usize {
    if offset + 4 <= buf.len()
        && buf[offset] == 0
        && buf[offset + 1] == 0
        && buf[offset + 2] == 0
        && buf[offset + 3] == 1
    {
        4
    } else {
        3
    }
}

fn classify_nal(bytes: Vec<u8>) -> NalUnit {
    let nal_type = bytes[0] & 0x1f;
    // Type 5 = coded slice of IDR picture. SPS (7) and PPS (8)
    // precede IDR in a keyframe boundary so flag them too — the
    // fmp4 pipeline in JS needs SPS/PPS before the first IDR to
    // build the init segment.
    let is_keyframe = nal_type == 5 || nal_type == 7 || nal_type == 8;
    NalUnit {
        bytes,
        nal_type,
        is_keyframe,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn splits_two_nals_with_4byte_start_code() {
        let mut sp = NalSplitter::new();
        let stream = [
            0, 0, 0, 1, 0x67, 0x42, 0x00, 0x0a, // SPS (type 7)
            0, 0, 0, 1, 0x68, 0xce, 0x06, 0xe2, // PPS (type 8)
            0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00, // IDR (type 5)
        ];
        let mut nals = sp.push(&stream);
        nals.extend(sp.flush());
        assert_eq!(nals.len(), 3);
        assert_eq!(nals[0].nal_type, 7);
        assert_eq!(nals[1].nal_type, 8);
        assert_eq!(nals[2].nal_type, 5);
        assert!(nals.iter().all(|n| n.is_keyframe));
    }

    #[test]
    fn handles_3byte_start_code() {
        let mut sp = NalSplitter::new();
        let stream = [0, 0, 1, 0x61, 0xab, 0, 0, 1, 0x61, 0xcd];
        let mut nals = sp.push(&stream);
        nals.extend(sp.flush());
        assert_eq!(nals.len(), 2);
        // Type 1 = non-IDR slice — not a keyframe.
        assert_eq!(nals[0].nal_type, 1);
        assert!(!nals[0].is_keyframe);
    }

    #[test]
    fn buffers_partial_nal_across_pushes() {
        let mut sp = NalSplitter::new();
        let first = sp.push(&[0, 0, 0, 1, 0x67, 0x42]);
        assert!(first.is_empty());
        let second = sp.push(&[0, 0x0a, 0, 0, 0, 1, 0x68]);
        // First push contained only one start code; second push
        // introduces a second, so the SPS NAL can now emerge.
        assert_eq!(second.len(), 1);
        assert_eq!(second[0].nal_type, 7);
    }
}
