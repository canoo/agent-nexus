import json, os, pathlib, subprocess, tempfile, sys
binary=str(pathlib.Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory() as tmp:
    root=pathlib.Path(tmp)
    agy=root/'agy'
    agy.write_text('#!/bin/sh\nprintf "STUB_AGY:%s:%s\\n" "$1" "$2"\n')
    agy.chmod(0o700)
    env=dict(os.environ,PATH=tmp+':'+os.environ['PATH'],NEXUS_REPO=tmp,OLLAMA_HOST_URL='file:///invalid',NEXUS_ROUTE_RETRIES='0')
    def control(op,params,ok=True):
        req=dict(schemaVersion=1,operation=op,confirm=True,params=params)
        p=subprocess.run([binary,'control','--json'],input=json.dumps(req),text=True,capture_output=True,env=env)
        result=json.loads(p.stdout)
        assert (p.returncode==0)==ok,(p.returncode,result,p.stderr)
        return result
    preview=control('route.preview',{'prompt':'write commit message'})
    assert '"destination": "local"' in json.dumps(preview),preview
    run=control('route.run',{'prompt':'design authentication','allowCloud':True})
    assert 'STUB_AGY:--prompt-interactive:design authentication' in json.dumps(run),run
    control('route.run',{'prompt':'design authentication'},False)
    control('route.run',{'prompt':'write commit message'},False)
    fallback=control('route.run',{'prompt':'write commit message','allowCloud':True})
    assert 'STUB_AGY:--prompt-interactive:write commit message' in json.dumps(fallback),fallback
    p=subprocess.run([binary,'--unknown','hello'],capture_output=True,text=True,env=env)
    assert p.returncode==2,p
    print('PASS: binary route.preview local; route.run cloud stdio; cloud denial; local preflight denial; consented fallback; unknown option rejected')
