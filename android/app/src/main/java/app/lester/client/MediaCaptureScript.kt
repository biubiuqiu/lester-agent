package app.lester.client

// Android PermissionRequest identifies an origin, not the requesting frame.
// Inject before page scripts in every matching frame: generated previews also
// retain their HTTP Permissions-Policy and opaque iframe sandbox boundaries.
internal val mediaCaptureScript = """
    (() => {
      const main = window.top === window;
      const unavailable = () => new DOMException('Microphone capture is limited to the main workspace.', 'NotAllowedError');
      const devices = window.MediaDevices?.prototype;
      const original = devices?.getUserMedia;
      if (typeof original !== 'function') return;
      const streams = new Set();
      let generation = 0;
      const stop = () => {
        generation++;
        for (const stream of streams) for (const track of stream.getTracks()) track.stop();
        streams.clear();
      };
      Object.defineProperty(devices, 'getUserMedia', {configurable:false, writable:false, value:function(constraints) {
        if (!main) return Promise.reject(unavailable());
        const started = generation;
        return original.call(this, constraints).then(stream => {
          if (started !== generation || document.visibilityState !== 'visible') {
            for (const track of stream.getTracks()) track.stop();
            throw new DOMException('Microphone capture was cancelled.', 'AbortError');
          }
          for (const previous of streams) if (previous.getTracks().every(track => track.readyState === 'ended')) streams.delete(previous);
          streams.add(stream);
          for (const track of stream.getTracks()) track.addEventListener('ended', () => {
            if (stream.getTracks().every(item => item.readyState === 'ended')) streams.delete(stream);
          });
          return stream;
        });
      }});
      for (const name of ['getUserMedia', 'webkitGetUserMedia', 'mozGetUserMedia']) {
        if (typeof navigator[name] === 'function') Object.defineProperty(Object.getPrototypeOf(navigator), name, {
          configurable:false, writable:false, value:function(constraints, success, failure) { if (typeof failure === 'function') failure(unavailable()); }
        });
      }
      if (main) {
        window.addEventListener('lester:native-media-stop', stop);
        window.addEventListener('pagehide', stop);
        document.addEventListener('visibilitychange', () => { if (document.visibilityState !== 'visible') stop(); });
      }
    })();
""".trimIndent()
