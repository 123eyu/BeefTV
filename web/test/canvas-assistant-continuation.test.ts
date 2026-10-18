import {expect,test} from "bun:test";
import {canContinueAssistant,continueAssistantOnce} from "@/pages/canvas/use-canvas-assistant-continuation";
import type {AssistantContinuation} from "@/services/api/assistant-continuation";

const row:AssistantContinuation={id:"receipt",canvasId:"canvas",sessionId:"session",proposalId:"gp-one",sourceTurnId:"source",requestId:"generation-fixed",turnId:"fixed",message:"【自动继续】检查已生成结果",permissionMode:"canvas",state:"claimed",taskIds:["task"],nodeIds:["actual"]};
const controller=()=>({canvasId:"canvas",sessionId:"session",historyLoaded:true,streaming:false,sessionBusy:false,awaitingReceipt:false,send:async()=>{}});

test("automatic admission waits for loaded, idle, matching canvas and session",()=>{
 expect(canContinueAssistant(controller(),row)).toBe(true);
 for(const patch of [{historyLoaded:false},{streaming:true},{sessionBusy:true},{awaitingReceipt:true},{sessionId:"other"},{canvasId:"other"}])expect(canContinueAssistant({...controller(),...patch},row)).toBe(false);
});

test("automatic dispatch uses fixed receipt and actual result references, leaving draft inputs untouched",async()=>{
 const draft={text:"我还没有发送的改法",attachments:["reference"],skills:["skill"],selection:["unrelated"]};
 const original=structuredClone(draft);const calls:unknown[][]=[];
 const active={...controller(),send:async(...args:unknown[])=>{calls.push(args);}};
 expect(await continueAssistantOnce(row,{current:()=>active,claim:async()=>row,onDispatch:()=>{}})).toBe(true);
 expect(calls).toEqual([[row.message,["actual"],[],{continuation:row,permissionMode:"canvas"}]]);
 expect(draft).toEqual(original);
});

for(const event of ["unmount","account-change","stop","new-message","session-change","canvas-change"]){
 test(`${event} while claim is pending prevents dispatch or steering`,async()=>{
  let active:ReturnType<typeof controller>|null=controller();let sends=0;
  active.send=async()=>{sends++;};
  let release!:(value:AssistantContinuation)=>void;
  const pending=new Promise<AssistantContinuation>(resolve=>{release=resolve;});
  const execution=continueAssistantOnce(row,{current:()=>active,claim:()=>pending,onDispatch:()=>{}});
  if(event==="new-message")active.streaming=true;
  else if(event==="session-change")active.sessionId="new";
  else if(event==="canvas-change")active.canvasId="new";
  else active=null;
  release(row);expect(await execution).toBe(false);expect(sends).toBe(0);
 });
}

test("cancelled, finished, mismatched receipts and unknown claims never send",async()=>{
 for(const claimed of [{...row,state:"cancelled" as const},{...row,state:"done" as const},{...row,id:"other"},{...row,sessionId:"other"}]){
  let sends=0;const active={...controller(),send:async()=>{sends++;}};
  expect(await continueAssistantOnce(row,{current:()=>active,claim:async()=>claimed,onDispatch:()=>{}})).toBe(false);expect(sends).toBe(0);
 }
 await expect(continueAssistantOnce(row,{current:controller,claim:async()=>{throw new Error("unknown");},onDispatch:()=>{throw new Error("must not dispatch");}})).rejects.toThrow("unknown");
});
