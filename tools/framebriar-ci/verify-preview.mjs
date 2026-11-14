// Dedicated capability entrypoint: absent from older frontend-only trusted revisions.
// Never select deployment verification through an optional flag an older bridge ignores.
import {fileURLToPath} from 'node:url'
import {verifyEvidence} from './bridge.mjs'
export const verifyPreview=(input,fetcher)=>verifyEvidence({...input,subject:'develop',prNumber:0,requirePreview:true},fetcher)
if(process.argv[1]&&fileURLToPath(import.meta.url)===process.argv[1]){
 try{
  const result=await verifyPreview({repository:process.env.GITHUB_REPOSITORY,targetSha:process.env.TARGET_SHA,trustedWorkflowSha:process.env.TRUSTED_WORKFLOW_SHA,token:process.env.GH_TOKEN})
  console.log(`Verified completed protected Preview for ${result.sha}; run ${result.run_id}, attempt ${result.run_attempt}.`)
 }catch{
  console.error('No acceptable exact-SHA protected Preview deployment evidence. No frontend-only fallback is allowed.')
  process.exitCode=1
 }
}
