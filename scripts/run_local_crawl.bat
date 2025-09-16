@REM go run .\cmd\tts ^
@REM   --kokoro-addr="localhost:50051" --kokoro-insecure ^
@REM   --in="out\chapters\interlude-strategists-at-sea-pt-1" --recursive ^
@REM   --out="out\audio" ^
@REM   --voice="en-us" --rate=24000 --speed=1.0 --format=wav ^
@REM   --final=mp3


go run .\cmd\gwp --url="http://localhost:8080/sample.html" --max=1 --out=out --chapter-fmt=txt
