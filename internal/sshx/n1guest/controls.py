"""Fixed configured-resolver example.com DNS/public HTTPS positives. Discard response data."""
import ipaddress, json, math, os, re, signal, socket, sys, time
import subprocess
UUID=re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\Z')
class Invalid(ValueError): pass

def require(condition):
    if not condition: raise Invalid()

def pairs(items):
    result={}
    for key,value in items:
        require(key not in result); result[key]=value
    return result

def bad(_): raise Invalid()

def decode(raw,limit=4096):
    require(type(raw) is bytes and 0<len(raw)<=limit)
    try: return json.loads(raw.decode('utf-8','strict'),object_pairs_hook=pairs,parse_constant=bad)
    except (ValueError,RecursionError,UnicodeError): raise Invalid() from None

def exact(value,keys): require(type(value) is dict and set(value)==set(keys))

def binding(value):
    exact(value,('version','domain','session_id','backend_kind','backend_object','generation'))
    require(type(value['version']) is int and value['version']==1 and value['domain']=='n1qualification' and value['backend_kind']=='tart')
    for name in ('session_id','generation'): require(type(value[name]) is str and UUID.fullmatch(value[name]))
    require(value['session_id']!=value['generation'])
    require(type(value['backend_object']) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,64}',value['backend_object']))
    return value


ENV={'PATH':'/usr/bin:/bin','LANG':'C','LC_ALL':'C','HOME':'/root'}
DNS=['/usr/bin/resolvectl','--cache=no','--synthesize=no','--zone=no','--protocol=dns','--type=A','query','example.com']
HTTPS=['/usr/bin/curl','--disable','--silent','--fail','--head','--proto','=https','--max-redirs','0','--connect-timeout','3','--max-time','5','--noproxy','*','https://example.com/']

def bounded_run(argv):
    # Response bytes go directly to the null device, never through a buffer.
    process=subprocess.Popen(argv,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,env=ENV,close_fds=True)
    try: return process.wait(timeout=6)
    except subprocess.TimeoutExpired:
        process.kill(); process.wait(timeout=1); return 1

def controls(request,runner=bounded_run):
    exact(request,('binding',)); b=binding(request['binding'])
    results=[]
    for command in (DNS,HTTPS):
        try: code=runner(list(command)); results.append('ok' if type(code) is int and code==0 else 'failed')
        except Exception: results.append('failed')
    return {'version':1,'binding':b,'code':'controls','dns':results[0],'https':results[1]}

def main(argv=None,stdin=None,stdout=None):
    argv=sys.argv[1:] if argv is None else argv; stdin=sys.stdin.buffer if stdin is None else stdin; stdout=sys.stdout if stdout is None else stdout
    result={'version':1,'code':'invalid_binding'}; code=2
    try: require(argv==[]); result=controls(decode(stdin.read(4097))); code=0
    except Exception: pass
    stdout.write(json.dumps(result,separators=(',',':'))+'\n'); stdout.flush(); return code
if __name__=='__main__':
    signal.signal(signal.SIGALRM,signal.SIG_DFL); signal.alarm(15); raise SystemExit(main())
