go run .\cmd\tts ^
  --kokoro-addr="localhost:50051" --kokoro-insecure ^
  --in="out\chapters\interlude-strategists-at-sea-pt-1" --recursive ^
  --out="out\audio" ^
  --voice="en-us" --rate=24000 --speed=1.0 --format=wav ^
  --final=mp3