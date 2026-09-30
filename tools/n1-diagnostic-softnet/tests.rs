use super::*;
use std::net::Ipv4Addr;
use std::cell::RefCell;
pub(crate) const CM:[u8; 6]=[2,0,0,0,0,2];
pub(crate) const PM:[u8; 6]=[2,0,0,0,0,3];
pub(crate) const GM:[u8; 6]=[2,0,0,0,0,1];
pub(crate) const CA:[u8; 4]=[192,168,64,2];
pub(crate) const PA:[u8; 4]=[192,168,64,3];
pub(crate) const GA:[u8; 4]=[192,168,64,1];
fn uid(n:u8)->String {
    format!("00000000-0000-4000-8000-{n:012x}")
}
fn binding(n:u8,a:[u8;4],m:[u8;6])->wire::SessionBinding {
    wire::SessionBinding{
        domain:"n1qualification".into(),session_id:uid(n),generation:uid(n+10),backend_kind:"tart".into(),backend_object:format!("n1-{n}"),address:a,mac:m
    }
}
pub(crate) fn arm()->wire::Arm {
    wire::Arm{
        version:1,kind:"ARM".into(),generation:uid(11),nonce:uid(30),operation_id:uid(40),candidate:binding(1,CA,CM),control:binding(2,PA,PM),gateway:GA,duration_ms:1000,control_provenance:"host_backend_pinned_owner".into()
    }
}
pub(crate) fn arp(src:[u8;4],dst:[u8;4],sm:[u8;6],dm:[u8;6],op:u8)->Vec<u8>{
    let mut f=vec![0;     42];
    f[..6].copy_from_slice(&dm);
    f[6..12].copy_from_slice(&sm);
    f[12..22].copy_from_slice(&[8,6,0,1,8,0,6,4,0,op]);
    f[22..28].copy_from_slice(&sm);
    f[28..32].copy_from_slice(&src);
    f[38..42].copy_from_slice(&dst);
    f
}
fn sum(p:&[u8])->u16 {
    let mut n=0u32;
    for c in p.chunks(2){
        n+=u16::from_be_bytes([c[0],*c.get(1).unwrap_or(&0)])as u32;
    }while n>>16!=0{
        n=(n&65535)+(n>>16);
    }!(n as u16)
}
fn ip(src:[u8;4],dst:[u8;4],proto:u8,p:&[u8],sm:[u8;6])->Vec<u8>{
    let mut f=vec![0;     34+p.len()];
    f[..6].copy_from_slice(&CM);
    f[6..12].copy_from_slice(&sm);
    f[12..14].copy_from_slice(&[8,0]);
    f[14]=0x45;
    f[16..18].copy_from_slice(&((20+p.len())as u16).to_be_bytes());
    f[22]=64;
    f[23]=proto;
    f[26..30].copy_from_slice(&src);
    f[30..34].copy_from_slice(&dst);
    f[34..].copy_from_slice(p);
    let c=sum(&f[14..34]);
    f[24..26].copy_from_slice(&c.to_be_bytes());
    f
}
fn tcp()->Vec<u8>{
    tcp_to(PA)
}
fn dhcp(a:[u8;4],ttl:u32)->Vec<u8>{
    let mut d=vec![0;     240];
    d[..3].copy_from_slice(&[2,1,6]);
    d[16..20].copy_from_slice(&a);
    d[28..34].copy_from_slice(&CM);
    d[236..240].copy_from_slice(&[99,130,83,99]);
    d.extend([53,1,5,54,4]);
    d.extend(GA);
    d.extend([51,4]);
    d.extend(ttl.to_be_bytes());
    d.push(255);
    let mut u=vec![0;     8];
    u[..4].copy_from_slice(&[0,67,0,68]);
    u[4..6].copy_from_slice(&((8+d.len())as u16).to_be_bytes());
    u.extend(d);
    ip(GA,a,17,&u,GM)
}
fn tcp_to(dst:[u8;4])->Vec<u8>{
    let mut t=vec![0;     20];
    t[..4].copy_from_slice(&[0x9c,0x40,0,22]);
    t[7]=1;
    t[12]=0x50;
    t[13]=2;
    let mut p=Vec::new();
    p.extend(CA);
    p.extend(dst);
    p.extend([0,6,0,20]);
    p.extend(&t);
    let c=sum(&p);
    t[16..18].copy_from_slice(&c.to_be_bytes());
    ip(CA,dst,6,&t,CM)
}
pub(crate) fn leased()->boxwarden_policy::Policy {
    let mut p=boxwarden_policy::Policy::new(CM,GA.into());
    p.host(&dhcp(CA,600),&[GA.into()],0);
    p.bind_candidate(CA.into());
    p
}
pub(crate) fn oracle()->canonical::Policy {
    let mut p=canonical::Policy::new(CM,GA.into());
    p.host(&dhcp(CA,600),&[GA.into()],0);
    p
}
fn observer()->dispatch::Observer {
    let mut o=dispatch::Observer::default();
    o.activate(arm());
    o
}
#[test]
fn canonical_guest_equivalence_raw_results_bytes_order_and_followup_state() {
    let frames=[arp(CA,PA,CM,PM,1),arp(CA,GA,CM,GM,1),tcp(),arp(CA,PA,[2,0,0,0,9,9],PM,1),arp(CA,[8,8,8,8],CM,PM,1),tcp_to([8,8,8,8])];
    for f in frames {
        for fallback in [false,true] {
            for write_mode in 0..4 {
                for refresh_fail in [false,true] {
                    let mut p=leased();
                    let mut q=oracle();
                    let a=RefCell::new(Vec::new());
                    let b=RefCell::new(Vec::new());
                    let mut o=observer();
                    let refresh=||{
                        a.borrow_mut().push("refresh");
                        if refresh_fail{
                            Err(1u8)
                        }else{
                            Ok((vec![GA.into()],vec![]))
                        }
                    };
                    let r=dispatch::dispatch_guest(&mut p,&f,1,&mut o,refresh,|bytes|{
                        assert_eq!(bytes,&f);a.borrow_mut().push("fallback");fallback
                    },|bytes|{
                        assert_eq!(bytes,&f);a.borrow_mut().push("write");if write_mode==3{
                            Err(2u8)
                        }else{
                            Ok(if write_mode==0{
                                f.len()
                            }else if write_mode==1{
                                f.len()-1
                            }else{
                                f.len()+1
                            })
                        }
                    },|_|dispatch::WriteError::VmnetFailure).map_err(|e|match e{
                        dispatch::DispatchError::Refresh(e)|dispatch::DispatchError::Write(e)=>e
                    });
                    let expected=q.forward_with_refresh(&f,1,||{
                        b.borrow_mut().push("refresh");if refresh_fail{
                            Err(1u8)
                        }else{
                            Ok((vec![GA.into()],vec![]))
                        }
                    },|bytes|{
                        assert_eq!(bytes,&f);b.borrow_mut().push("fallback");fallback
                    },|bytes|{
                        assert_eq!(bytes,&f);b.borrow_mut().push("write");if write_mode==3{
                            Err(2u8)
                        }else{
                            Ok(())
                        }
                    });
                    assert_eq!(r,expected);
                    assert_eq!(a.into_inner(),b.into_inner());
                    assert!(o.accounting());
                    for future in [2,700] {
                        let test=arp(CA,GA,CM,GM,1);
                        assert_eq!(p.guest(&test,&[GA.into()],future)as u8,q.guest(&test,&[GA.into()],future)as u8);
                    }
                }
            }
        }
    }
}
#[test]
fn guest_peer_arp_zero_write_and_cached_syn_independent_deny() {
    for (f,row) in [(arp(CA,PA,CM,PM,1),0),(tcp(),2)] {
        let mut p=leased();
        let mut o=observer();
        let r=dispatch::dispatch_guest(&mut p,&f,1,&mut o,||Ok::<_,u8>((vec![GA.into()],vec![])),|_|false,|_|panic!("denied packet wrote"),|_:&u8|dispatch::WriteError::IoOther);
        assert!(matches!(r,Ok(false)));
        assert_eq!(o.counters.vm_identified[row],1);
        assert_eq!(o.counters.vm_write_attempts[row],0);
        assert_eq!(o.counters.vm_outcomes[row][if row==0{
            0
        }else{
            1
        }],1);
        assert!(o.summary(0,1_000_000_000,true).unwrap().complete);
    }
}
#[test]
fn choosing_lease_expiry_sender_and_short_circuit_predicates() {
    for (now,src,mac,target,reason,lease,gateway,local) in [(1,CA,CM,PA,4,3,1,1),(700,CA,CM,PA,2,2,0,0),(1,CA,PM,PA,2,3,0,0),(1,CA,CM,GA,3,3,2,0),(1,PA,CM,PA,2,3,0,0)] {
        let mut p=leased();
        let f=arp(src,target,mac,GM,1);
        p.guest_with_broadcasts(&f,&[GA.into()],&[],now);
        let m=p.metadata();
        assert_eq!((m.arp_reason,m.lease,m.gateway_equal,m.local_contains),(reason,lease,gateway,local));
    }
    let mut p=leased();
    p.guest_with_broadcasts(&arp(CA,PA,CM,PM,1),&[],&[],700);
    p.guest_with_broadcasts(&arp(CA,PA,CM,PM,1),&[],&[],701);
    assert_eq!(p.metadata().lease,1);
    p.bind_candidate(PA.into());
    p.host(&dhcp(CA,600),&[],702);
    p.guest_with_broadcasts(&arp(CA,PA,CM,PM,1),&[],&[],703);
    assert_eq!(p.metadata().lease,4);
}
#[test]
fn canonical_host_equivalence_enobufs_ignored_policy_boolean_and_order() {
    for opcode in [1,2,3] {
        for error in [0u8,11,12] {
            for short in [false,true] {
                for allow in [false,true] {
                    for refresh_fail in [false,true] {
                        let f=arp(PA,CA,PM,CM,opcode);
                        let mut p=leased();
                        let mut q=oracle();
                        let mut o=observer();
                        let mut enobufs=false;
                        let mut expected_enobufs=false;
                        let a=RefCell::new(Vec::new());
                        let b=RefCell::new(Vec::new());
                        let r=dispatch::dispatch_host(&mut p,&f,&mut o,|_|{
                            a.borrow_mut().push("allow");allow
                        },||{
                            a.borrow_mut().push("refresh");if refresh_fail{
                                Err(1u8)
                            }else{
                                Ok((vec![GA.into()],vec![]))
                            }
                        },||{
                            a.borrow_mut().push("now");1
                        },|bytes|{
                            assert_eq!(bytes,&f);a.borrow_mut().push("write");if error!=0{
                                Err(error)
                            }else{
                                Ok(f.len()-usize::from(short))
                            }
                        },|e|if *e==11{
                            dispatch::WriteError::IoEnobufs
                        }else{
                            dispatch::WriteError::IoOther
                        },&mut enobufs).map_err(|e|match e{
                            dispatch::DispatchError::Refresh(e)|dispatch::DispatchError::Write(e)=>e
                        });
                        b.borrow_mut().push("allow");
                        let expected=if !allow{
                            Ok(())
                        }else{
                            b.borrow_mut().push("refresh");
                            if refresh_fail{
                                Err(1)
                            }else{
                                b.borrow_mut().push("now");
                                let _=q.host(&f,&[GA.into()],1);
                                b.borrow_mut().push("write");
                                if error==11{
                                    expected_enobufs=true;
                                    Ok(())
                                }else if error==0{
                                    Ok(())
                                }else{
                                    Err(error)
                                }
                            }
                        };
                        assert_eq!(r,expected);
                        assert_eq!(enobufs,expected_enobufs);
                        assert_eq!(a.into_inner(),b.into_inner());
                        assert!(o.accounting());
                    }
                }
            }
        }
    }
}
#[test]
fn host_same_ip_all_six_classes_before_filters_and_request_separation() {
    for unexpected in [false,true] {
        for (dest,d) in [(CM,0),([255;6],1),(GM,2)] {
            let mut f=arp(PA,CA,PM,dest,2);
            if unexpected{
                f[22]^=2;
            }let mut p=leased();
            let mut o=observer();
            let mut e=false;
            dispatch::dispatch_host(&mut p,&f,&mut o,|_|false,||panic!("filter denial refreshed"),||panic!("filter denial sampled clock"),|_|panic!("filter denial wrote"),|_:&u8|dispatch::WriteError::IoOther,&mut e).unwrap_or_else(|_:dispatch::DispatchError<u8,u8>|panic!());
            let row=d+if unexpected{
                3
            }else{
                0
            };
            assert_eq!(o.counters.host_reply_class[row],1);
            assert_eq!(o.counters.host_class_outcomes[row][0],1);
            assert_eq!(o.counters.refresh_results[1],[1,0,0]);
        }
    }
    let mut p=leased();
    let mut o=observer();
    let mut e=false;
    let f=arp(PA,CA,PM,CM,1);
    dispatch::dispatch_host(&mut p,&f,&mut o,|_|true,||Ok::<_,u8>((vec![],vec![])),||1,|b|Ok::<_,u8>(b.len()),|_|dispatch::WriteError::IoOther,&mut e).unwrap();
    assert_eq!(o.counters.host_pair_arp_request,1);
    assert_eq!(o.counters.host_reply_class,[0;6]);
}
#[test]
fn refresh_failure_short_success_unsupported_and_capacity_sticky_incomplete() {
    let mut p=leased();
    let mut o=observer();
    let f=arp(CA,PA,CM,PM,1);
    let _=dispatch::dispatch_guest(&mut p,&f,1,&mut o,||Err::<(Vec<Ipv4Addr>,Vec<Ipv4Addr>),_>(1),|_|panic!(),|_|panic!(),|_:&u8|dispatch::WriteError::IoOther);
    assert_eq!(o.counters.vm_lease_state[0],[1,0,0,0,0]);
    assert!(!o.summary(0,1,true).unwrap().complete);
    let mut o=observer();
    let mut e=false;
    let f=arp(PA,CA,PM,CM,2);
    dispatch::dispatch_host(&mut p,&f,&mut o,|_|true,||Ok::<_,u8>((vec![],vec![])),||1,|b|Ok::<_,u8>(b.len()+1),|_|dispatch::WriteError::IoOther,&mut e).unwrap();
    assert!(o.invalid[8]);
    assert_eq!(o.counters.host_class_outcomes[0][3],1);
    assert!(!o.summary(0,1,true).unwrap().complete);
    let mut o=observer();
    let mut f=arp(PA,CA,PM,CM,3);
    dispatch::dispatch_host(&mut p,&f,&mut o,|_|false,||Ok::<_,u8>((vec![],vec![])),||1,|_|Ok::<_,u8>(42),|_|dispatch::WriteError::IoOther,&mut e).unwrap();
    assert_eq!(o.counters.recognized_unsupported_pair[1],1);
    assert!(!o.summary(0,1,true).unwrap().complete);
    f.truncate(20);
    o.dropped_outer(1,&f);
    assert!(o.invalid[4]);
    let mut o=observer();
    for _ in 0..4097{
        let f=arp(PA,CA,PM,CM,2);
        dispatch::dispatch_host(&mut p,&f,&mut o,|_|false,||Ok::<_,u8>((vec![],vec![])),||1,|_|Ok::<_,u8>(42),|_|dispatch::WriteError::IoOther,&mut e).unwrap();
    }assert!(o.overflow);
    assert_eq!(o.counters.host_dispatch_started,4096);
    assert!(!o.summary(0,1,true).unwrap().complete);
}
#[test]
fn strict_wire_unknown_duplicates_missing_lexical_pair_nonce_and_interval() {
    let a=arm();
    let good=serde_json::to_vec(&a).unwrap();
    assert_eq!(wire::decode::<wire::Arm>(&good).unwrap(),a);
    let raw=String::from_utf8(good).unwrap();
    for bad in [raw.replace("\"version\":1","\"version\":1,\"version\":1"),raw.replace("\"duration_ms\":1000","\"duration_ms\":-0"),raw.replace("\"duration_ms\":1000","\"duration_ms\":1e3"),raw.replace("\"duration_ms\":1000","\"duration_ms\":1000.0"),raw.replace("\"duration_ms\":1000","\"duration_ms\":4294967296"),raw.replace("\"version\":1,",""),raw.replace("\"version\":1","\"unknown\":1,\"version\":1"),format!("{raw} {{}}"),raw.replace("\"domain\":\"n1qualification\"","\"domain\":\"n1qualification\",\"domain\":\"n1qualification\"")]{
        assert!(wire::decode::<wire::Arm>(bad.as_bytes()).is_err(),"accepted {bad}");
    }
    let selector=wire::Selector{
        generation:a.generation.clone(),nonce:a.nonce.clone()
    };
    assert!(a.valid(&selector,CM,GA));
    for duration in [999,30001]{
        let mut bad=a.clone();
        bad.duration_ms=duration;
        assert!(!bad.valid(&selector,CM,GA));
    }
    let mut bad=a.clone();
    bad.nonce=uid(99);
    assert!(!bad.valid(&selector,CM,GA));
    bad=a.clone();
    bad.control.address=CA;
    assert!(!bad.valid(&selector,CM,GA));
    for s in ["@boxwarden-host-containment","@boxwarden-n1-diagnostic:bad:bad","@boxwarden-n1-diagnostic:00000000-0000-0000-0000-000000000000:00000000-0000-0000-0000-000000000000"]{
        assert!(wire::Selector::parse(s).is_none());
    }
}
#[test]
fn r1_conservative_serialized_bound_maximizes_valid_binding_spellings() {
    fn measure(mut a:wire::Arm,lease_valid:bool)->(usize,usize) {
        a.candidate.backend_object="x".repeat(128);
        a.control.backend_object="y".repeat(128);
        a.duration_ms=30000;
        let mut o=dispatch::Observer::default();
        o.activate(a.clone());
        let mut s=o.summary(u64::MAX,u64::MAX,true).unwrap();
        s.candidate_lease_valid=lease_valid;
        assert!(!s.loss&&!s.overflow&&s.invalid_flags.iter().all(|flag|!*flag));
        let mut value=serde_json::to_value(&s.counters).unwrap();
        fn fill(v:&mut serde_json::Value,key:&str){
            match v{
                serde_json::Value::Number(_)=>*v=serde_json::json!(if key.ends_with("bytes"){
                    u64::MAX
                }else{
                    4096
                }),serde_json::Value::Array(a)=>for v in a{
                    fill(v,key)
                },serde_json::Value::Object(o)=>for(k,v)in o{
                    fill(v,k)
                },_=>{
                }
            }
        }fill(&mut value,"");
        s.counters=serde_json::from_value(value).unwrap();
        s.complete=false;
        let hello=wire::Hello{
            version:1,kind:"HELLO".into(),generation:a.generation.clone(),nonce:a.nonce.clone(),candidate_mac:a.candidate.mac,gateway:a.gateway
        };
        let armed=wire::Armed{
            version:1,kind:"ARMED".into(),generation:a.generation.clone(),nonce:a.nonce.clone(),operation_id:a.operation_id.clone(),candidate:a.candidate.clone(),control:a.control.clone(),gateway:a.gateway,duration_ms:30000,control_provenance:a.control_provenance.clone(),armed_offset_ns:u64::MAX,candidate_lease_valid:true
        };
        let h=wire::frame(&hello).unwrap();
        let r=wire::frame(&armed).unwrap();
        let b=wire::frame(&s).unwrap();
        assert!(h.len()+r.len()+b.len()<=wire::MAX_OUTPUT);
        assert!(b.len()<=4100);
        assert!(wire::frame(&a).unwrap().len()<=wire::MAX_FRAME+4);
        assert_eq!(wire::decode::<wire::Arm>(&serde_json::to_vec(&a).unwrap()).unwrap(),a);
        (b.len()-4,h.len()+r.len()+b.len())
    }
    let original=measure(arm(),true);
    let mut widest=arm();
    widest.candidate.address=[192,168,255,254];
    widest.control.address=[192,168,255,253];
    widest.gateway=[192,168,255,252];
    widest.candidate.mac=[254,255,255,255,255,254];
    widest.control.mac=[254,255,255,255,255,253];
    let selector=wire::Selector{
        generation:widest.generation.clone(),nonce:widest.nonce.clone()
    };
    assert!(widest.valid(&selector,widest.candidate.mac,widest.gateway));
    let encoded=serde_json::to_string(&widest).unwrap();
    assert!(encoded.contains("\"address\":[192,168,255,254]"));
    assert!(encoded.contains("\"mac\":[254,255,255,255,255,254]"));
    // Independent width maxima deliberately overapproximate jointly reachable
    // counter/boolean combinations. ARMED's lease validity is a fixed true.
    // SUMMARY's variable booleans use false, whose JSON spelling is longest.
    let bound=measure(widest,false);
    assert!(bound.0>original.0);
    assert!(bound.1>original.1);
    println!("original binding fixture SUMMARY={} child_total={}; conservative serialized upper bound SUMMARY={} child_total={}",original.0,original.1,bound.0,bound.1);
}
