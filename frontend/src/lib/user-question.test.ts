import { strict as assert } from "node:assert";
import { test } from "node:test";
import { validQuestionAnswer, type UserQuestion } from "./user-question";
import { sameInteractionResponse } from "./interaction-response";

test("recommendations do not answer a question; selections and other text are independently retained",()=>{
 const question:UserQuestion={id:"features",prompt:"Which features?",multi_select:true,options:[{id:"queue",label:"Queue",recommended:true},{id:"recap",label:"Recap"}]};
 assert.equal(validQuestionAnswer(question),false);
 assert.equal(validQuestionAnswer(question,{option_ids:["queue","recap"],text:"Keep my custom details"}),true);
 assert.equal(validQuestionAnswer(question,{text:"Something else"}),true);
 assert.equal(validQuestionAnswer(question,{option_ids:["queue","queue"]}),false);
 assert.equal(validQuestionAnswer(question,{option_ids:["unoffered"]}),false);
 assert.equal(validQuestionAnswer(question,{option_id:"queue"}),false);
 assert.equal(validQuestionAnswer({...question,multi_select:false},{option_ids:["queue"]}),false);
 assert.equal(sameInteractionResponse({answers:{features:{option_ids:[],option_id:"",text:"Other"}}},{answers:{features:{text:"Other"}}}),true);
 assert.equal(sameInteractionResponse({answers:{features:{option_ids:["queue","recap"],text:"Extra"}}},{answers:{features:{option_ids:["queue"],text:"Extra"}}}),false);
});
