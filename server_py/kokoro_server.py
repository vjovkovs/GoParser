import os, sys
sys.path.insert(0, os.path.dirname(__file__))

import io, time, wave, argparse, grpc, warnings
from concurrent import futures
import numpy as np

from kokoro import KPipeline
from api.tts.v1 import tts_pb2, tts_pb2_grpc

DEFAULT_SR = 24000

# Silence specific noisy warnings
warnings.filterwarnings("ignore",
    message="dropout option adds dropout.*num_layers greater than 1",
    category=UserWarning,
)
warnings.filterwarnings("ignore",
    message=".*torch.nn.utils.weight_norm.*deprecated.*",
    category=FutureWarning,
)

VOICE_CATALOG = [
    ("af_heart",  "a", "female", "Heart (American Female)"),
    ("af_bella",  "a", "female", "Bella (American Female)"),
    ("am_michael","a", "male",   "Michael (American Male)"),
    ("bf_emma",   "b", "female", "Emma (British Female)"),
    ("bm_george", "b", "male",   "George (British Male)"),
]
VOICE_ALIASES = {
    "en": "af_heart", "en-us": "af_heart", "american": "af_heart",
    "en-gb": "bf_emma", "british": "bf_emma",
}

def lang_from_voice(voice: str, default_lang: str = "a") -> str:
    v = (voice or "").strip().lower()
    if "_" in v:
        return v[0]
    mapped = VOICE_ALIASES.get(v)
    return mapped[0] if mapped else default_lang

class TTSServicer(tts_pb2_grpc.TTSServicer):
    def __init__(self, default_lang: str, default_voice: str, repo_id: str):
        self.default_lang  = default_lang
        self.default_voice = default_voice
        self.repo_id       = repo_id
        self._pipelines    = {}  # lang_code -> KPipeline

    def _get_pipeline(self, lang_code: str) -> KPipeline:
        if lang_code not in self._pipelines:
            # 👇 pass repo_id to suppress “Defaulting repo_id …” warning
            self._pipelines[lang_code] = KPipeline(lang_code=lang_code, repo_id=self.repo_id)
        return self._pipelines[lang_code]

    def ListVoices(self, request, context):
        resp = tts_pb2.ListVoicesResponse()
        for vid, lang, gender, display in VOICE_CATALOG:
            resp.voices.add(id=vid, lang_code=lang, gender=gender, display=display)
        for alias, maps_to in VOICE_ALIASES.items():
            resp.aliases.add(alias=alias, maps_to=maps_to)
        return resp

    def Synthesize(self, request, context):
        text         = request.text or ""
        voice_req    = (request.voice or self.default_voice).strip()
        voice        = VOICE_ALIASES.get(voice_req.lower(), voice_req)
        lang         = lang_from_voice(voice, self.default_lang)
        sample_rate  = request.sample_rate_hz or DEFAULT_SR
        speed        = request.speed or 1.0
        if not text.strip():
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "empty text")

        pipe = self._get_pipeline(lang)

        parts = []
        for _, _, audio in pipe(text, voice=voice, speed=float(speed)):
            parts.append(np.asarray(audio, dtype=np.float32).flatten())
        if not parts:
            context.abort(grpc.StatusCode.INTERNAL, "kokoro produced no audio")
        wav = np.concatenate(parts, axis=0)

        data = audio_to_wav_bytes(wav, sample_rate=sample_rate)
        CHUNK = 32 * 1024
        for i in range(0, len(data), CHUNK):
            yield tts_pb2.AudioChunk(audio=data[i:i+CHUNK])

def audio_to_wav_bytes(audio: np.ndarray, sample_rate: int = DEFAULT_SR) -> bytes:
    audio = np.clip(audio, -1.0, 1.0)
    pcm = (audio * 32767.0).astype(np.int16)
    import io, wave
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(sample_rate)
        w.writeframes(pcm.tobytes())
    return buf.getvalue()

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--listen", default="0.0.0.0:50051")
    p.add_argument("--lang",   default="a")
    p.add_argument("--voice",  default="af_heart")
    p.add_argument("--repo",   default=os.environ.get("KOKORO_REPO_ID", "hexgrad/Kokoro-82M"),
                   help="HuggingFace repo id for Kokoro (default: hexgrad/Kokoro-82M)")
    args = p.parse_args()

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    tts_pb2_grpc.add_TTSServicer_to_server(TTSServicer(args.lang, args.voice, args.repo), server)
    server.add_insecure_port(args.listen)
    print(f"[kokoro] gRPC on {args.listen} (lang={args.lang} voice={args.voice} repo={args.repo})", flush=True)
    server.start()
    try:
        while True: time.sleep(86400)
    except KeyboardInterrupt:
        server.stop(0)

if __name__ == "__main__":
    main()
