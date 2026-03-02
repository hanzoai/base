import{S as St,i as At,s as Lt,V as Mt,W as Ht,X as Q,j as d,d as Re,t as Y,a as x,I as jt,Z as Ft,_ as Nt,C as zt,$ as Ut,D as Jt,n as u,o as t,m as Te,E as Wt,G as Gt,u as a,A as _,v as i,c as Fe,w as b,J as Bt,b as Kt,l as ee,p as Vt}from"./index-CzlWNNWT.js";function Et(s,o,n){const c=s.slice();return c[6]=o[n],c}function Ot(s,o,n){const c=s.slice();return c[6]=o[n],c}function Pt(s,o){let n,c,y;function f(){return o[5](o[6])}return{key:s,first:null,c(){n=a("button"),n.textContent=`${o[6].code} `,b(n,"class","tab-item"),ee(n,"active",o[1]===o[6].code),this.first=n},m(r,h){u(r,n,h),c||(y=Vt(n,"click",f),c=!0)},p(r,h){o=r,h&10&&ee(n,"active",o[1]===o[6].code)},d(r){r&&d(n),c=!1,y()}}}function It(s,o){let n,c,y,f;return c=new Ht({props:{content:o[6].body}}),{key:s,first:null,c(){n=a("div"),Fe(c.$$.fragment),y=i(),b(n,"class","tab-item"),ee(n,"active",o[1]===o[6].code),this.first=n},m(r,h){u(r,n,h),Te(c,n,null),t(n,y),f=!0},p(r,h){o=r,(!f||h&10)&&ee(n,"active",o[1]===o[6].code)},i(r){f||(x(c.$$.fragment,r),f=!0)},o(r){Y(c.$$.fragment,r),f=!1},d(r){r&&d(n),Re(c)}}}function Xt(s){var mt,pt,bt,ht,ft,_t,yt,$t;let o,n,c=s[0].name+"",y,f,r,h,B,v,z,Be,F,E,Ee,O,Oe,Pe,te,le,w,ae,P,se,I,oe,H,ne,U,ie,q,ce,Ie,re,S,J,He,$,W,Se,de,Ae,C,G,Le,ue,Me,K,je,me,Ne,k,ze,pe,Ue,Je,We,V,Ge,X,Ke,be,Ve,he,Xe,fe,Ze,m,_e,Qe,ye,Ye,$e,xe,ge,et,ve,tt,De,lt,at,st,Ce,ot,R,ke,A,we,T,L,D=[],nt=new Map,it,M,g=[],ct=new Map,j,qe,rt;w=new Mt({props:{js:`
        import Base from 'base';

        const base = new Base('${s[2]}');

        ...

        const batch = base.createBatch();

        batch.collection('${(mt=s[0])==null?void 0:mt.name}').create({ ... });
        batch.collection('${(pt=s[0])==null?void 0:pt.name}').update('RECORD_ID', { ... });
        batch.collection('${(bt=s[0])==null?void 0:bt.name}').delete('RECORD_ID');
        batch.collection('${(ht=s[0])==null?void 0:ht.name}').upsert({ ... });

        const result = await batch.send();
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${s[2]}');

        ...

        final batch = base.createBatch();

        batch.collection('${(ft=s[0])==null?void 0:ft.name}').create(body: { ... });
        batch.collection('${(_t=s[0])==null?void 0:_t.name}').update('RECORD_ID', body: { ... });
        batch.collection('${(yt=s[0])==null?void 0:yt.name}').delete('RECORD_ID');
        batch.collection('${($t=s[0])==null?void 0:$t.name}').upsert(body: { ... });

        final result = await batch.send();
    `}}),R=new Ht({props:{language:"javascript",content:`
                            const formData = new FormData();

                            formData.append("@jsonPayload", JSON.stringify({
                                requests: [
                                    {
                                        method: "POST",
                                        url: "/api/collections/${s[0].name}/records?fields=id",
                                        body: { someField: "test1" }
                                    },
                                    {
                                        method: "PATCH",
                                        url: "/api/collections/${s[0].name}/records/RECORD_ID",
                                        body: { someField: "test2" }
                                    }
                                ]
                            }))

                            // file for the first request
                            formData.append("requests.0.someFileField", new File(...))

                            // file for the second request
                            formData.append("requests.1.someFileField", new File(...))
                        `}});let Z=Q(s[3]);const dt=e=>e[6].code;for(let e=0;e<Z.length;e+=1){let l=Ot(s,Z,e),p=dt(l);nt.set(p,D[e]=Pt(p,l))}let N=Q(s[3]);const ut=e=>e[6].code;for(let e=0;e<N.length;e+=1){let l=Et(s,N,e),p=ut(l);ct.set(p,g[e]=It(p,l))}return{c(){o=a("h3"),n=_("Batch create/update/upsert/delete ("),y=_(c),f=_(")"),r=i(),h=a("div"),h.innerHTML="<p>Batch and transactional create/update/upsert/delete of multiple records in a single request.</p>",B=i(),v=a("div"),z=a("div"),z.innerHTML='<i class="ri-error-warning-line"></i>',Be=i(),F=a("div"),E=a("p"),Ee=_(`The batch Web API need to be explicitly enabled and configured from the
            `),O=a("a"),O.textContent="Dashboard settings",Oe=_("."),Pe=i(),te=a("p"),te.innerHTML=`Because this endpoint process the requests in a single DB transaction it could degrade the
            performance of your application if not used with proper care and configuration
            <em>(prefer smaller max processing and body size limits, avoid large file uploads over slow S3
                networks and custom hooks that communicate with slow external APIs)</em>.`,le=i(),Fe(w.$$.fragment),ae=i(),P=a("h6"),P.textContent="API details",se=i(),I=a("div"),I.innerHTML='<strong class="label label-primary">POST</strong> <div class="content">/api/batch</div>',oe=i(),H=a("div"),H.textContent="Body Parameters",ne=i(),U=a("p"),U.innerHTML=`Body parameters could be sent as <em>application/json</em> or <em>multipart/form-data</em>.
    <br/>
    File upload is supported only via <em>multipart/form-data</em> (see below for more details).`,ie=i(),q=a("table"),ce=a("thead"),ce.innerHTML='<tr><th>Param</th> <th width="80%">Description</th></tr>',Ie=i(),re=a("tbody"),S=a("tr"),J=a("td"),J.innerHTML='<div class="flex txt-nowrap"><span class="label label-success">Required</span> <span>requests</span></div>',He=i(),$=a("td"),W=a("span"),W.textContent="Array<Request>",Se=_(` - List of the requests to process.

                `),de=a("p"),de.textContent="The supported batch request actions are:",Ae=i(),C=a("ul"),G=a("li"),Le=_("record create - "),ue=a("code"),ue.textContent="POST /api/collections/{collection}/records",Me=i(),K=a("li"),je=_(`record update -
                        `),me=a("code"),me.textContent="PATCH /api/collections/{collection}/records/{id}",Ne=i(),k=a("li"),ze=_("record upsert - "),pe=a("code"),pe.textContent="PUT /api/collections/{collection}/records",Ue=i(),Je=a("br"),We=i(),V=a("small"),V.innerHTML='(the body must have <code class="txt-sm">id</code> field)',Ge=i(),X=a("li"),Ke=_(`record delete -
                        `),be=a("code"),be.textContent="DELETE /api/collections/{collection}/records/{id}",Ve=i(),he=a("p"),he.textContent="Each batch Request element have the following properties:",Xe=i(),fe=a("ul"),fe.innerHTML=`<li><code>url path</code> <em>(could include query parameters)</em></li> <li><code>method</code> <em>(GET, POST, PUT, PATCH, DELETE)</em></li> <li><code>headers</code> <br/> <em>(custom per-request <code>Authorization</code> header is not supported at the moment,
                            aka. all batch requests have the same auth state)</em></li> <li><code>body</code></li>`,Ze=i(),m=a("p"),_e=a("strong"),_e.textContent="NB!",Qe=_(` When the batch request is send as
                    `),ye=a("code"),ye.textContent="multipart/form-data",Ye=_(`, the regular batch action fields are expected to be
                    submitted as serialized json under the `),$e=a("code"),$e.textContent="@jsonPayload",xe=_(` field and file keys need
                    to follow the pattern `),ge=a("code"),ge.textContent="requests.N.fileField",et=_(` or
                    `),ve=a("code"),ve.textContent="requests[N].fileField",tt=i(),De=a("em"),De.textContent=`(this is usually handled transparently by the SDKs when their specific object notation
                        is used)
                    `,lt=_(`.
                    `),at=a("br"),st=_(`
                    If you don't use the SDKs or prefer manually to construct the `),Ce=a("code"),Ce.textContent="FormData",ot=_(`
                    body, then it could look something like:
                    `),Fe(R.$$.fragment),ke=i(),A=a("div"),A.textContent="Responses",we=i(),T=a("div"),L=a("div");for(let e=0;e<D.length;e+=1)D[e].c();it=i(),M=a("div");for(let e=0;e<g.length;e+=1)g[e].c();b(o,"class","m-b-sm"),b(h,"class","content txt-lg m-b-sm"),b(z,"class","icon"),b(O,"href","/settings"),b(F,"class","content"),b(v,"class","alert alert-warning"),b(P,"class","m-b-xs"),b(I,"class","api-route alert alert-success"),b(H,"class","section-title"),b(J,"valign","top"),b(W,"class","label"),b(V,"class","txt-hint"),b(q,"class","table-compact table-border m-t-xs m-b-base"),b(A,"class","section-title"),b(L,"class","tabs-header compact combined left"),b(M,"class","tabs-content"),b(T,"class","tabs")},m(e,l){u(e,o,l),t(o,n),t(o,y),t(o,f),u(e,r,l),u(e,h,l),u(e,B,l),u(e,v,l),t(v,z),t(v,Be),t(v,F),t(F,E),t(E,Ee),t(E,O),t(E,Oe),t(F,Pe),t(F,te),u(e,le,l),Te(w,e,l),u(e,ae,l),u(e,P,l),u(e,se,l),u(e,I,l),u(e,oe,l),u(e,H,l),u(e,ne,l),u(e,U,l),u(e,ie,l),u(e,q,l),t(q,ce),t(q,Ie),t(q,re),t(re,S),t(S,J),t(S,He),t(S,$),t($,W),t($,Se),t($,de),t($,Ae),t($,C),t(C,G),t(G,Le),t(G,ue),t(C,Me),t(C,K),t(K,je),t(K,me),t(C,Ne),t(C,k),t(k,ze),t(k,pe),t(k,Ue),t(k,Je),t(k,We),t(k,V),t(C,Ge),t(C,X),t(X,Ke),t(X,be),t($,Ve),t($,he),t($,Xe),t($,fe),t($,Ze),t($,m),t(m,_e),t(m,Qe),t(m,ye),t(m,Ye),t(m,$e),t(m,xe),t(m,ge),t(m,et),t(m,ve),t(m,tt),t(m,De),t(m,lt),t(m,at),t(m,st),t(m,Ce),t(m,ot),Te(R,m,null),u(e,ke,l),u(e,A,l),u(e,we,l),u(e,T,l),t(T,L);for(let p=0;p<D.length;p+=1)D[p]&&D[p].m(L,null);t(T,it),t(T,M);for(let p=0;p<g.length;p+=1)g[p]&&g[p].m(M,null);j=!0,qe||(rt=Wt(Gt.call(null,O)),qe=!0)},p(e,[l]){var vt,Dt,Ct,kt,wt,qt,Rt,Tt;(!j||l&1)&&c!==(c=e[0].name+"")&&jt(y,c);const p={};l&5&&(p.js=`
        import Base from 'base';

        const base = new Base('${e[2]}');

        ...

        const batch = base.createBatch();

        batch.collection('${(vt=e[0])==null?void 0:vt.name}').create({ ... });
        batch.collection('${(Dt=e[0])==null?void 0:Dt.name}').update('RECORD_ID', { ... });
        batch.collection('${(Ct=e[0])==null?void 0:Ct.name}').delete('RECORD_ID');
        batch.collection('${(kt=e[0])==null?void 0:kt.name}').upsert({ ... });

        const result = await batch.send();
    `),l&5&&(p.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[2]}');

        ...

        final batch = base.createBatch();

        batch.collection('${(wt=e[0])==null?void 0:wt.name}').create(body: { ... });
        batch.collection('${(qt=e[0])==null?void 0:qt.name}').update('RECORD_ID', body: { ... });
        batch.collection('${(Rt=e[0])==null?void 0:Rt.name}').delete('RECORD_ID');
        batch.collection('${(Tt=e[0])==null?void 0:Tt.name}').upsert(body: { ... });

        final result = await batch.send();
    `),w.$set(p);const gt={};l&1&&(gt.content=`
                            const formData = new FormData();

                            formData.append("@jsonPayload", JSON.stringify({
                                requests: [
                                    {
                                        method: "POST",
                                        url: "/api/collections/${e[0].name}/records?fields=id",
                                        body: { someField: "test1" }
                                    },
                                    {
                                        method: "PATCH",
                                        url: "/api/collections/${e[0].name}/records/RECORD_ID",
                                        body: { someField: "test2" }
                                    }
                                ]
                            }))

                            // file for the first request
                            formData.append("requests.0.someFileField", new File(...))

                            // file for the second request
                            formData.append("requests.1.someFileField", new File(...))
                        `),R.$set(gt),l&10&&(Z=Q(e[3]),D=Ft(D,l,dt,1,e,Z,nt,L,Nt,Pt,null,Ot)),l&10&&(N=Q(e[3]),zt(),g=Ft(g,l,ut,1,e,N,ct,M,Ut,It,null,Et),Jt())},i(e){if(!j){x(w.$$.fragment,e),x(R.$$.fragment,e);for(let l=0;l<N.length;l+=1)x(g[l]);j=!0}},o(e){Y(w.$$.fragment,e),Y(R.$$.fragment,e);for(let l=0;l<g.length;l+=1)Y(g[l]);j=!1},d(e){e&&(d(o),d(r),d(h),d(B),d(v),d(le),d(ae),d(P),d(se),d(I),d(oe),d(H),d(ne),d(U),d(ie),d(q),d(ke),d(A),d(we),d(T)),Re(w,e),Re(R);for(let l=0;l<D.length;l+=1)D[l].d();for(let l=0;l<g.length;l+=1)g[l].d();qe=!1,rt()}}}function Zt(s,o,n){let c,y,{collection:f}=o,r=200,h=[];const B=v=>n(1,r=v.code);return s.$$set=v=>{"collection"in v&&n(0,f=v.collection)},s.$$.update=()=>{s.$$.dirty&1&&n(4,y=Bt.dummyCollectionRecord(f)),s.$$.dirty&17&&f!=null&&f.id&&(h.push({code:200,body:JSON.stringify([{status:200,body:y},{status:200,body:Object.assign({},y,{id:y.id+"2"})}],null,2)}),h.push({code:400,body:`
                {
                  "status": 400,
                  "message": "Batch transaction failed.",
                  "data": {
                    "requests": {
                      "1": {
                        "code": "batch_request_failed",
                        "message": "Batch request failed.",
                        "response": {
                          "status": 400,
                          "message": "Failed to create record.",
                          "data": {
                            "id": {
                              "code": "validation_min_text_constraint",
                              "message": "Must be at least 3 character(s).",
                              "params": { "min": 3 }
                            }
                          }
                        }
                      }
                    }
                  }
                }
            `}),h.push({code:403,body:`
                {
                  "status": 403,
                  "message": "Batch requests are not allowed.",
                  "data": {}
                }
            `}))},n(2,c=Bt.getApiExampleUrl(Kt.baseURL)),[f,r,c,h,y,B]}class Yt extends St{constructor(o){super(),At(this,o,Zt,Xt,Lt,{collection:0})}}export{Yt as default};
