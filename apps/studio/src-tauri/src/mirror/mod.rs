//! Studio mirror backend. One module per target kind; each owns
//! its subprocess topology (scrcpy/ffmpeg, simctl/ffmpeg,
//! avfoundation/ffmpeg) and a parser that splits the resulting
//! H.264 byte stream into NAL units dispatched to JS over a
//! Tauri Channel.
//!
//! Android is the only live target today. Simulator and real-
//! device iOS surface typed errors so the UI can present a
//! "coming soon" state rather than a generic failure.

pub mod commands;
pub mod h264;
pub mod scrcpy;
pub mod sck;
pub mod session;

pub use commands::*;
pub use session::SessionRegistry;
