// This fixed local document has no native preload. File bytes are assigned only
// to the opaque inner sandbox, never inserted into this wrapper's DOM.
const nonce=location.hash.slice(1);
window.addEventListener('message',event=>{
  if(event.source!==parent||!nonce||event.data?.nonce!==nonce||event.data?.type!=='lester-preview'||typeof event.data.content!=='string'||event.data.content.length>2*1024*1024)return;
  const frame=document.getElementById('document');
  if(typeof event.data.title==='string')frame.title=event.data.title.slice(0,500);
  frame.srcdoc=`<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${event.data.content}`;
});
