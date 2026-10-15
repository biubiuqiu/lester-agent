import test from "node:test";
import assert from "node:assert/strict";
import { recapExcerpt, shouldOfferRecap, recapChanged } from "./session-recap";
test("away recaps require new evidence, sufficient absence and no unsent draft", () => {
 const saved = { enabled: true, seenAt: 100, eventId: 10, messageId: "last" };
 const next = { eventId: 11, messageId: "last" };
 assert.equal(shouldOfferRecap(saved,next,120100,false),true);
 assert.equal(shouldOfferRecap(saved,next,120100,true),false);
 assert.equal(shouldOfferRecap(saved,next,120099,false),false);
 assert.equal(shouldOfferRecap({...saved,enabled:false},next,130100,false),false);
 assert.equal(shouldOfferRecap({...saved,seenAt:0},next,130100,false),false);
 assert.equal(shouldOfferRecap(saved,{eventId:10,messageId:"last"},130100,false),false);
 assert.equal(recapChanged(saved,{eventId:9,messageId:"new message"}),true);
 assert.equal(shouldOfferRecap(saved,next,0,false),false);
});
test("recap excerpts are bounded and do not split Unicode characters", () => {
 assert.equal(recapExcerpt("short"),"short");
 const excerpt = recapExcerpt("😀".repeat(500));
 assert.equal(Array.from(excerpt).length,400);
 assert.ok(excerpt.endsWith("…"));
 assert.equal(recapExcerpt("a ".repeat(250)).length<=400,true);
});
