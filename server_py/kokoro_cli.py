
import argparse
import io
import json
import os
import re
import shutil
import subprocess
import sys
import time
import warnings
import wave
from pathlib import Path

import numpy as np
from kokoro import KPipeline  # pip install kokoro

DEFAULT_SR = 24000


# ----------------------------
# Logging & CI annotations
# ----------------------------

def _is_ci():
    return os.environ.get("GITHUB_ACTIONS") == "true"


def log(msg=""):
    print(msg, flush=True)


def notice(msg):
    if _is_ci():
        print(f"::notice::{msg}", flush=True)
    else:
        print(msg, flush=True)


def warn(msg):
    if _is_ci():
        print(f"::warning::{msg}", flush=True)
    else:
        print(f"WARNING: {msg}", flush=True)


def err(msg):
    if _is_ci():
        print(f"::error::{msg}", flush=True)
    else:
        print(f"ERROR: {msg}", flush=True)


# Silence noisy PyTorch warnings commonly emitted by Kokoro deps
warnings.filterwarnings(
    "ignore",
    message=r"dropout option adds dropout.*num_layers greater than 1",
    category=UserWarning,
)
warnings.filterwarnings(
    "ignore",
    message=r".*torch\.nn\.utils\.weight_norm.*deprecated.*",
    category=FutureWarning,
)


# ----------------------------
# Voice catalog
# ----------------------------

def load_catalog(catalog_path: Path) -> dict:
    with open(catalog_path, "r", encoding="utf-8") as f:
        return json.load(f)


def resolve_voice(catalog: dict, requested: str) -> tuple[str, str]:
    """
    Return (voice_id, lang_code) given a requested id or alias.
    Falls back to af_heart / 'a' if not found.
    """
    req = (requested or "").strip().lower()
    vid = catalog.get("aliases", {}).get(req, requested)
    for v in catalog.get("voices", []):
        if v["id"].lower() == (vid or "").lower():
            return v["id"], v.get("lang", "a")
    # Guess language pipeline from id like 'af_heart' -> 'a'
    if isinstance(vid, str) and "_" in vid:
        return vid, vid[0]
    return "af_heart", "a"


def list_voices(catalog: dict):
    print("Voices:")
    for v in catalog.get("voices", []):
        print(
            f"  {v['id']:<12}  lang={v.get('lang','?')}  "
            f"gender={v.get('gender','')}  {v.get('display','')}"
        )
    if catalog.get("aliases"):
        print("\nAliases:")
        for k, v in catalog["aliases"].items():
            print(f"  {k:<10} -> {v}")


# ----------------------------
# Files & text utils
# ----------------------------

def collect_inputs(path: Path, recursive: bool) -> list[Path]:
    if path.is_file():
        return [path] if path.suffix.lower() == ".txt" else []
    pat = "**/*.txt" if recursive else "*.txt"
    return sorted(path.glob(pat))


def slug(s: str) -> str:
    s = s.strip().lower()
    s = re.sub(r"[^\w\s-]+", "-", s)
    s = re.sub(r"[\s_]+", "-", s)
    s = s.strip("-")
    return (s or "audiofile")[:64]


def read_text(p: Path) -> str:
    return p.read_text(encoding="utf-8", errors="ignore")


def split_smart(text: str, max_chars: int) -> list[str]:
    """
    Soft sentence-based chunking with a hard wrap for outliers.
    """
    s = text.replace("\r\n", "\n")
    s = re.sub(r"[ \t\f\v]+", " ", s)

    # Greedy sentence-ish segments (+ paragraph breaks)
    sents = re.findall(r"(?s).+?(?:[\.!\?]+[\"\')\]]*\s+|\n{2,}|$)", s)

    chunks: list[str] = []
    cur: list[str] = []
    cur_len = 0

    def flush():
        nonlocal cur, cur_len, chunks
        if cur_len > 0:
            chunks.append("".join(cur).strip())
            cur, cur_len = [], 0

    for sen in sents:
        if cur_len + len(sen) > max_chars and cur_len > 0:
            flush()
        if len(sen) > max_chars:
            # Hard-wrap a very long sentence
            remains = sen
            while len(remains) > max_chars:
                cut = remains.rfind(" ", 0, max_chars)
                if cut <= 0:
                    cut = max_chars
                cur.append(remains[:cut] + "\n")
                cur_len += cut + 1
                flush()
                remains = remains[cut:].lstrip()
            if remains:
                cur.append(remains)
                cur_len += len(remains)
        else:
            cur.append(sen)
            cur_len += len(sen)

    if cur_len > 0:
        flush()

    return chunks


# ----------------------------
# Audio I/O
# ----------------------------

def audio_to_wav_bytes(audio: np.ndarray, sample_rate: int) -> bytes:
    pcm = np.clip(audio, -1.0, 1.0)
    pcm = (pcm * 32767.0).astype(np.int16)
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)  # 16-bit
        w.setframerate(sample_rate)
        w.writeframes(pcm.tobytes())
    return buf.getvalue()


def write_wav_file(path: Path, audio: np.ndarray, sample_rate: int):
    path.parent.mkdir(parents=True, exist_ok=True)
    data = audio_to_wav_bytes(audio, sample_rate)
    path.write_bytes(data)


def combine_wavs(wavs: list[Path], out_wav: Path):
    """
    Concatenate multiple WAV files. All must share the same params.
    """
    assert wavs, "no wavs to combine"
    out_wav.parent.mkdir(parents=True, exist_ok=True)

    def read_wave(p: Path):
        with wave.open(str(p), "rb") as r:
            params = r.getparams()  # nchannels, sampwidth, framerate, nframes, ...
            frames = r.readframes(r.getnframes())
        return params, frames

    p0, f0 = read_wave(wavs[0])
    with wave.open(str(out_wav), "wb") as w:
        w.setparams(p0)
        w.writeframes(f0)
        for p in wavs[1:]:
            pi, fi = read_wave(p)
            if (pi.nchannels, pi.sampwidth, pi.framerate) != (
                p0.nchannels,
                p0.sampwidth,
                p0.framerate,
            ):
                raise RuntimeError(f"format mismatch in {p}")
            w.writeframes(fi)


def which(name: str) -> str | None:
    return shutil.which(name)


def encode_mp3(in_wav: Path, out_mp3: Path) -> tuple[bool, str]:
    """
    Encode to MP3 via lame or ffmpeg. Returns (ok, encoder_name).
    """
    lame = which("lame")
    ffm = which("ffmpeg")
    cmd, name = None, ""
    if lame:
        cmd, name = [lame, "--silent", "-V2", str(in_wav), str(out_mp3)], "lame"
    elif ffm:
        cmd, name = [
            ffm,
            "-y",
            "-i",
            str(in_wav),
            "-vn",
            "-ar",
            "44100",
            "-ac",
            "2",
            "-b:a",
            "192k",
            str(out_mp3),
        ], "ffmpeg"
    else:
        return False, ""
    out_mp3.parent.mkdir(parents=True, exist_ok=True)
    ok = subprocess.call(cmd) == 0
    return ok, name


# ----------------------------
# Kokoro synth
# ----------------------------

def synth_file(pipeline: KPipeline, text: str, voice: str, speed: float) -> np.ndarray:
    """
    Kokoro yields (graphemes, phonemes, audio) segments. Concatenate audio to one array.
    """
    parts = []
    for _, _, audio in pipeline(text, voice=voice, speed=float(speed)):
        parts.append(np.asarray(audio, dtype=np.float32).flatten())
    if not parts:
        raise RuntimeError("kokoro produced no audio")
    return np.concatenate(parts, axis=0)


# ----------------------------
# Progress helpers
# ----------------------------

def _progress_enabled(mode: str) -> bool:
    if mode == "none":
        return False
    return True  # "auto" and "simple" both behave the same: simple text progress


def _print_progress(prefix: str, i: int, n: int, start_t: float, quiet: bool):
    if quiet:
        return
    elapsed = time.perf_counter() - start_t
    rate = i / elapsed if elapsed > 0 else 0.0
    eta = (n - i) / rate if rate > 0 else 0.0
    print(f"{prefix} [{i}/{n}]  elapsed={elapsed:0.1f}s  eta={eta:0.1f}s", flush=True)


# ----------------------------
# Commands
# ----------------------------

def cmd_list(args):
    catalog = load_catalog(Path(args.catalog))
    list_voices(catalog)


def cmd_synth(args):
    # verbosity
    verbose = bool(args.verbose and not args.quiet)
    quiet = bool(args.quiet and not args.verbose)

    # load voices & resolve
    catalog = load_catalog(Path(args.catalog))
    voice_id, lang = resolve_voice(catalog, args.voice)
    if verbose:
        notice(f"Using voice '{voice_id}' (lang={lang}) from catalog {args.catalog}")

    # collect inputs
    inp = Path(args.input)
    if not inp.exists():
        err(f"Input path not found: {inp}")
        sys.exit(2)
    inputs = collect_inputs(inp, args.recursive)
    if not inputs:
        err("No .txt files found.")
        sys.exit(1)

    # dry-run: show chunk counts then exit
    if args.dry_run:
        notice("Dry run: computing chunks only")
        for f in inputs:
            text = read_text(f)
            chunks = split_smart(text, max(400, args.max))
            print(f"- {f} -> {len(chunks)} chunks (max={args.max})")
        return

    # load Kokoro pipeline
    if verbose:
        log(f"Loading Kokoro pipeline lang={lang} repo={args.repo} ...")
    t0 = time.perf_counter()
    pipe = KPipeline(lang_code=lang, repo_id=args.repo)
    if verbose:
        log(f"Pipeline ready in {time.perf_counter() - t0:0.2f}s")

    out_root = Path(args.out)
    summary = {
        "repo": args.repo,
        "voice": voice_id,
        "rate": args.rate,
        "speed": args.speed,
        "inputs": [],
        "encoder": None,
    }

    prog_on = _progress_enabled(args.progress)
    any_failed = False

    for f in inputs:
        started = time.perf_counter()
        base = slug(f.stem)
        item_root = out_root / base
        parts_dir = item_root / "parts"
        parts_dir.mkdir(parents=True, exist_ok=True)

        if not quiet:
            print(f"\n=== Synthesizing {f} -> {item_root} ===", flush=True)

        text = read_text(f)
        chunks = split_smart(text, max(400, args.max))
        if verbose:
            log(f"Chunks: {len(chunks)}  (max={args.max})")

        wav_paths: list[Path] = []
        ok_file = True

        for i, ch in enumerate(chunks, 1):
            try:
                audio = synth_file(pipe, ch, voice=voice_id, speed=args.speed)
                wav_p = parts_dir / f"{base}-part-{i:04d}.wav"
                write_wav_file(wav_p, audio, args.rate)
                wav_paths.append(wav_p)
                if prog_on:
                    _print_progress(base, i, len(chunks), started, quiet)
                time.sleep(0.02)  # small breather to be polite
            except Exception as ex:
                any_failed = True
                ok_file = False
                err(f"{f} chunk {i}: {ex}")
                break

        file_entry = {
            "input": str(f),
            "base": base,
            "chunks": len(wav_paths),
            "parts_dir": str(parts_dir),
            "full_wav": None,
            "full_mp3": None,
            "status": "ok" if ok_file else "failed",
            "elapsed_sec": round(time.perf_counter() - started, 3),
        }

        # Combine + encode
        if ok_file and args.final.lower() in ("wav", "mp3"):
            try:
                full_wav = item_root / f"{base}-full.wav"
                combine_wavs(wav_paths, full_wav)
                if not quiet:
                    notice(f"[WAV] {full_wav}")
                file_entry["full_wav"] = str(full_wav)

                if args.final.lower() == "mp3":
                    success, enc_name = encode_mp3(full_wav, item_root / f"{base}-full.mp3")
                    if success:
                        file_entry["full_mp3"] = str(item_root / f"{base}-full.mp3")
                        summary["encoder"] = summary["encoder"] or enc_name
                        if not quiet:
                            notice(f"[MP3] {file_entry['full_mp3']} (via {enc_name})")
                    else:
                        warn("MP3 encoder not found or failed; kept WAV only.")
            except Exception as ex:
                any_failed = True
                file_entry["status"] = "failed"
                err(f"Combine/encode failed for {f}: {ex}")

        summary["inputs"].append(file_entry)

    # write JSON summary if requested
    if args.summary_json:
        try:
            outp = Path(args.summary_json)
            outp.parent.mkdir(parents=True, exist_ok=True)
            outp.write_text(json.dumps(summary, indent=2), encoding="utf-8")
            if not quiet:
                notice(f"Summary written to {outp}")
        except Exception as ex:
            warn(f"Failed to write summary JSON: {ex}")

    # final tallies
    total_ok = sum(1 for x in summary["inputs"] if x["status"] == "ok")
    total_fail = sum(1 for x in summary["inputs"] if x["status"] != "ok")
    if not quiet:
        print(f"\nDone. OK: {total_ok}  Failed: {total_fail}", flush=True)

    if any_failed:
        sys.exit(1)


# ----------------------------
# Entrypoint / argparse
# ----------------------------

def main():
    ap = argparse.ArgumentParser(
        prog="kokoro-cli", description="Kokoro TTS CLI (no server)"
    )
    sub = ap.add_subparsers(dest="cmd", required=True)

    # list
    ap_list = sub.add_parser("list", help="List voices & aliases")
    ap_list.add_argument(
        "--catalog", default="voices/voices.json", help="voices catalog JSON"
    )
    ap_list.set_defaults(func=cmd_list)

    # synth
    ap_s = sub.add_parser("synth", help="Synthesize from a .txt file or directory")
    ap_s.add_argument("--input", "-i", required=True, help="Path to .txt or directory")
    ap_s.add_argument("--out", "-o", default="out/audio", help="Output directory")
    ap_s.add_argument(
        "--voice",
        "-v",
        default="af_heart",
        help="Voice id or alias from catalog (e.g., af_heart, en-us)",
    )
    ap_s.add_argument("--rate", type=int, default=DEFAULT_SR, help="Sample rate (Hz)")
    ap_s.add_argument("--speed", type=float, default=1.0, help="Speaking rate (1.0 normal)")
    ap_s.add_argument("--max", type=int, default=1800, help="Max chars per chunk")
    ap_s.add_argument(
        "--final",
        choices=["wav", "mp3", "none"],
        default="mp3",
        help="Final combined output type",
    )
    ap_s.add_argument(
        "--recursive", action="store_true", help="Recurse into subfolders (if input is a dir)"
    )
    ap_s.add_argument(
        "--repo",
        default=os.environ.get("KOKORO_REPO_ID", "hexgrad/Kokoro-82M"),
        help="HuggingFace repo id for Kokoro model",
    )
    ap_s.add_argument(
        "--catalog", default="voices/voices.json", help="voices catalog JSON"
    )
    # feedback & reports
    ap_s.add_argument(
        "--dry-run", action="store_true", help="Parse inputs & show planned chunks, then exit"
    )
    ap_s.add_argument("--verbose", action="store_true", help="Verbose output")
    ap_s.add_argument("--quiet", action="store_true", help="Minimal output")
    ap_s.add_argument(
        "--summary-json", default="", help="Write a JSON summary report to this path"
    )
    ap_s.add_argument(
        "--progress",
        choices=["auto", "simple", "none"],
        default="auto",
        help="Progress display mode",
    )
    ap_s.set_defaults(func=cmd_synth)

    args = ap.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
