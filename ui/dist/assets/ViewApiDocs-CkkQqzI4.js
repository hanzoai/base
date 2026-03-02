import{S as lt,i as st,s as nt,V as at,W as tt,X as K,j as r,d as W,t as z,a as U,I as ke,Z as Ge,_ as it,C as ot,$ as rt,D as dt,n as d,o as l,m as X,u as a,A as _,v as b,c as Z,w as m,J as Ke,b as ct,l as Y,p as ut}from"./index-CzlWNNWT.js";import{F as pt}from"./FieldsQueryParam-BjMsFtDw.js";function We(i,s,n){const o=i.slice();return o[6]=s[n],o}function Xe(i,s,n){const o=i.slice();return o[6]=s[n],o}function Ze(i){let s;return{c(){s=a("p"),s.innerHTML="Requires superuser <code>Authorization:TOKEN</code> header",m(s,"class","txt-hint txt-sm txt-right")},m(n,o){d(n,s,o)},d(n){n&&r(s)}}}function Ye(i,s){let n,o,k;function u(){return s[5](s[6])}return{key:i,first:null,c(){n=a("button"),n.textContent=`${s[6].code} `,m(n,"class","tab-item"),Y(n,"active",s[2]===s[6].code),this.first=n},m(c,f){d(c,n,f),o||(k=ut(n,"click",u),o=!0)},p(c,f){s=c,f&20&&Y(n,"active",s[2]===s[6].code)},d(c){c&&r(n),o=!1,k()}}}function et(i,s){let n,o,k,u;return o=new tt({props:{content:s[6].body}}),{key:i,first:null,c(){n=a("div"),Z(o.$$.fragment),k=b(),m(n,"class","tab-item"),Y(n,"active",s[2]===s[6].code),this.first=n},m(c,f){d(c,n,f),X(o,n,null),l(n,k),u=!0},p(c,f){s=c,(!u||f&20)&&Y(n,"active",s[2]===s[6].code)},i(c){u||(U(o.$$.fragment,c),u=!0)},o(c){z(o.$$.fragment,c),u=!1},d(c){c&&r(n),W(o)}}}function ft(i){var Ve,Je;let s,n,o=i[0].name+"",k,u,c,f,w,C,ee,V=i[0].name+"",te,$e,le,F,se,I,ne,$,J,ye,N,E,we,ae,Q=i[0].name+"",ie,Ce,oe,Fe,re,S,de,x,ce,M,ue,R,pe,Re,P,D,fe,De,be,Oe,h,Te,A,Ee,Ae,Be,me,Ie,_e,Se,xe,Me,he,Pe,qe,B,ge,q,ve,O,H,y=[],He=new Map,Le,L,g=[],je=new Map,T;F=new at({props:{js:`
        import Base from 'base';

        const base = new Base('${i[3]}');

        ...

        const record = await base.collection('${(Ve=i[0])==null?void 0:Ve.name}').getOne('RECORD_ID', {
            expand: 'relField1,relField2.subRelField',
        });
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${i[3]}');

        ...

        final record = await base.collection('${(Je=i[0])==null?void 0:Je.name}').getOne('RECORD_ID',
          expand: 'relField1,relField2.subRelField',
        );
    `}});let v=i[1]&&Ze();A=new tt({props:{content:"?expand=relField1,relField2.subRelField"}}),B=new pt({});let G=K(i[4]);const ze=e=>e[6].code;for(let e=0;e<G.length;e+=1){let t=Xe(i,G,e),p=ze(t);He.set(p,y[e]=Ye(p,t))}let j=K(i[4]);const Ue=e=>e[6].code;for(let e=0;e<j.length;e+=1){let t=We(i,j,e),p=Ue(t);je.set(p,g[e]=et(p,t))}return{c(){s=a("h3"),n=_("View ("),k=_(o),u=_(")"),c=b(),f=a("div"),w=a("p"),C=_("Fetch a single "),ee=a("strong"),te=_(V),$e=_(" record."),le=b(),Z(F.$$.fragment),se=b(),I=a("h6"),I.textContent="API details",ne=b(),$=a("div"),J=a("strong"),J.textContent="GET",ye=b(),N=a("div"),E=a("p"),we=_("/api/collections/"),ae=a("strong"),ie=_(Q),Ce=_("/records/"),oe=a("strong"),oe.textContent=":id",Fe=b(),v&&v.c(),re=b(),S=a("div"),S.textContent="Path Parameters",de=b(),x=a("table"),x.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr></thead> <tbody><tr><td>id</td> <td><span class="label">String</span></td> <td>ID of the record to view.</td></tr></tbody>',ce=b(),M=a("div"),M.textContent="Query parameters",ue=b(),R=a("table"),pe=a("thead"),pe.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',Re=b(),P=a("tbody"),D=a("tr"),fe=a("td"),fe.textContent="expand",De=b(),be=a("td"),be.innerHTML='<span class="label">String</span>',Oe=b(),h=a("td"),Te=_(`Auto expand record relations. Ex.:
                `),Z(A.$$.fragment),Ee=_(`
                Supports up to 6-levels depth nested relations expansion. `),Ae=a("br"),Be=_(`
                The expanded relations will be appended to the record under the
                `),me=a("code"),me.textContent="expand",Ie=_(" property (eg. "),_e=a("code"),_e.textContent='"expand": {"relField1": {...}, ...}',Se=_(`).
                `),xe=a("br"),Me=_(`
                Only the relations to which the request user has permissions to `),he=a("strong"),he.textContent="view",Pe=_(" will be expanded."),qe=b(),Z(B.$$.fragment),ge=b(),q=a("div"),q.textContent="Responses",ve=b(),O=a("div"),H=a("div");for(let e=0;e<y.length;e+=1)y[e].c();Le=b(),L=a("div");for(let e=0;e<g.length;e+=1)g[e].c();m(s,"class","m-b-sm"),m(f,"class","content txt-lg m-b-sm"),m(I,"class","m-b-xs"),m(J,"class","label label-primary"),m(N,"class","content"),m($,"class","alert alert-info"),m(S,"class","section-title"),m(x,"class","table-compact table-border m-b-base"),m(M,"class","section-title"),m(R,"class","table-compact table-border m-b-base"),m(q,"class","section-title"),m(H,"class","tabs-header compact combined left"),m(L,"class","tabs-content"),m(O,"class","tabs")},m(e,t){d(e,s,t),l(s,n),l(s,k),l(s,u),d(e,c,t),d(e,f,t),l(f,w),l(w,C),l(w,ee),l(ee,te),l(w,$e),d(e,le,t),X(F,e,t),d(e,se,t),d(e,I,t),d(e,ne,t),d(e,$,t),l($,J),l($,ye),l($,N),l(N,E),l(E,we),l(E,ae),l(ae,ie),l(E,Ce),l(E,oe),l($,Fe),v&&v.m($,null),d(e,re,t),d(e,S,t),d(e,de,t),d(e,x,t),d(e,ce,t),d(e,M,t),d(e,ue,t),d(e,R,t),l(R,pe),l(R,Re),l(R,P),l(P,D),l(D,fe),l(D,De),l(D,be),l(D,Oe),l(D,h),l(h,Te),X(A,h,null),l(h,Ee),l(h,Ae),l(h,Be),l(h,me),l(h,Ie),l(h,_e),l(h,Se),l(h,xe),l(h,Me),l(h,he),l(h,Pe),l(P,qe),X(B,P,null),d(e,ge,t),d(e,q,t),d(e,ve,t),d(e,O,t),l(O,H);for(let p=0;p<y.length;p+=1)y[p]&&y[p].m(H,null);l(O,Le),l(O,L);for(let p=0;p<g.length;p+=1)g[p]&&g[p].m(L,null);T=!0},p(e,[t]){var Ne,Qe;(!T||t&1)&&o!==(o=e[0].name+"")&&ke(k,o),(!T||t&1)&&V!==(V=e[0].name+"")&&ke(te,V);const p={};t&9&&(p.js=`
        import Base from 'base';

        const base = new Base('${e[3]}');

        ...

        const record = await base.collection('${(Ne=e[0])==null?void 0:Ne.name}').getOne('RECORD_ID', {
            expand: 'relField1,relField2.subRelField',
        });
    `),t&9&&(p.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[3]}');

        ...

        final record = await base.collection('${(Qe=e[0])==null?void 0:Qe.name}').getOne('RECORD_ID',
          expand: 'relField1,relField2.subRelField',
        );
    `),F.$set(p),(!T||t&1)&&Q!==(Q=e[0].name+"")&&ke(ie,Q),e[1]?v||(v=Ze(),v.c(),v.m($,null)):v&&(v.d(1),v=null),t&20&&(G=K(e[4]),y=Ge(y,t,ze,1,e,G,He,H,it,Ye,null,Xe)),t&20&&(j=K(e[4]),ot(),g=Ge(g,t,Ue,1,e,j,je,L,rt,et,null,We),dt())},i(e){if(!T){U(F.$$.fragment,e),U(A.$$.fragment,e),U(B.$$.fragment,e);for(let t=0;t<j.length;t+=1)U(g[t]);T=!0}},o(e){z(F.$$.fragment,e),z(A.$$.fragment,e),z(B.$$.fragment,e);for(let t=0;t<g.length;t+=1)z(g[t]);T=!1},d(e){e&&(r(s),r(c),r(f),r(le),r(se),r(I),r(ne),r($),r(re),r(S),r(de),r(x),r(ce),r(M),r(ue),r(R),r(ge),r(q),r(ve),r(O)),W(F,e),v&&v.d(),W(A),W(B);for(let t=0;t<y.length;t+=1)y[t].d();for(let t=0;t<g.length;t+=1)g[t].d()}}}function bt(i,s,n){let o,k,{collection:u}=s,c=200,f=[];const w=C=>n(2,c=C.code);return i.$$set=C=>{"collection"in C&&n(0,u=C.collection)},i.$$.update=()=>{i.$$.dirty&1&&n(1,o=(u==null?void 0:u.viewRule)===null),i.$$.dirty&3&&u!=null&&u.id&&(f.push({code:200,body:JSON.stringify(Ke.dummyCollectionRecord(u),null,2)}),o&&f.push({code:403,body:`
                    {
                      "status": 403,
                      "message": "Only superusers can access this action.",
                      "data": {}
                    }
                `}),f.push({code:404,body:`
                {
                  "status": 404,
                  "message": "The requested resource wasn't found.",
                  "data": {}
                }
            `}))},n(3,k=Ke.getApiExampleUrl(ct.baseURL)),[u,o,c,k,f,w]}class ht extends lt{constructor(s){super(),st(this,s,bt,ft,nt,{collection:0})}}export{ht as default};
