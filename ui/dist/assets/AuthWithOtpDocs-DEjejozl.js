import{S as be,i as _e,s as ve,W as ge,X as V,j as b,d as x,t as E,a as J,I as re,Z as de,_ as Ee,C as ue,$ as ze,D as he,n as _,o as n,m as ee,u as d,v as T,A as R,c as te,w as g,J as $e,l as N,p as ke,V as Qe,Y as De,b as Ke,a0 as Me}from"./index-DQaqjr2E.js";import{F as Xe}from"./FieldsQueryParam-po9OWZMy.js";function Be(s,t,e){const l=s.slice();return l[4]=t[e],l}function Ie(s,t,e){const l=s.slice();return l[4]=t[e],l}function We(s,t){let e,l=t[4].code+"",h,i,r,o;function m(){return t[3](t[4])}return{key:s,first:null,c(){e=d("button"),h=R(l),i=T(),g(e,"class","tab-item"),N(e,"active",t[1]===t[4].code),this.first=e},m(v,C){_(v,e,C),n(e,h),n(e,i),r||(o=ke(e,"click",m),r=!0)},p(v,C){t=v,C&4&&l!==(l=t[4].code+"")&&re(h,l),C&6&&N(e,"active",t[1]===t[4].code)},d(v){v&&b(e),r=!1,o()}}}function Fe(s,t){let e,l,h,i;return l=new ge({props:{content:t[4].body}}),{key:s,first:null,c(){e=d("div"),te(l.$$.fragment),h=T(),g(e,"class","tab-item"),N(e,"active",t[1]===t[4].code),this.first=e},m(r,o){_(r,e,o),ee(l,e,null),n(e,h),i=!0},p(r,o){t=r;const m={};o&4&&(m.content=t[4].body),l.$set(m),(!i||o&6)&&N(e,"active",t[1]===t[4].code)},i(r){i||(J(l.$$.fragment,r),i=!0)},o(r){E(l.$$.fragment,r),i=!1},d(r){r&&b(e),x(l)}}}function Ze(s){let t,e,l,h,i,r,o,m=s[0].name+"",v,C,F,B,I,D,z,M,U,P,y,q,$,L,Y,A,K,j,a,k,O,Z,u,f,S,w,X,we,Te,Oe,fe,ye,Pe,le,pe,ae,me,G,se,Q=[],Se=new Map,qe,oe,H=[],Ce=new Map,ne;O=new ge({props:{content:"?expand=relField1,relField2.subRelField"}}),le=new Xe({props:{prefix:"record."}});let ce=V(s[2]);const Ae=c=>c[4].code;for(let c=0;c<ce.length;c+=1){let p=Ie(s,ce,c),W=Ae(p);Se.set(W,Q[c]=We(W,p))}let ie=V(s[2]);const Re=c=>c[4].code;for(let c=0;c<ie.length;c+=1){let p=Be(s,ie,c),W=Re(p);Ce.set(W,H[c]=Fe(W,p))}return{c(){t=d("div"),e=d("strong"),e.textContent="POST",l=T(),h=d("div"),i=d("p"),r=R("/api/collections/"),o=d("strong"),v=R(m),C=R("/auth-with-otp"),F=T(),B=d("div"),B.textContent="Body Parameters",I=T(),D=d("table"),D.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>otpId</span></div></td> <td><span class="label">String</span></td> <td>The id of the OTP request.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>The one-time password.</td></tr></tbody>',z=T(),M=d("div"),M.textContent="Query parameters",U=T(),P=d("table"),y=d("thead"),y.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',q=T(),$=d("tbody"),L=d("tr"),Y=d("td"),Y.textContent="expand",A=T(),K=d("td"),K.innerHTML='<span class="label">String</span>',j=T(),a=d("td"),k=R(`Auto expand record relations. Ex.:
                `),te(O.$$.fragment),Z=R(`
                Supports up to 6-levels depth nested relations expansion. `),u=d("br"),f=R(`
                The expanded relations will be appended to the record under the
                `),S=d("code"),S.textContent="expand",w=R(" property (eg. "),X=d("code"),X.textContent='"expand": {"relField1": {...}, ...}',we=R(`).
                `),Te=d("br"),Oe=R(`
                Only the relations to which the request user has permissions to `),fe=d("strong"),fe.textContent="view",ye=R(" will be expanded."),Pe=T(),te(le.$$.fragment),pe=T(),ae=d("div"),ae.textContent="Responses",me=T(),G=d("div"),se=d("div");for(let c=0;c<Q.length;c+=1)Q[c].c();qe=T(),oe=d("div");for(let c=0;c<H.length;c+=1)H[c].c();g(e,"class","label label-primary"),g(h,"class","content"),g(t,"class","alert alert-success"),g(B,"class","section-title"),g(D,"class","table-compact table-border m-b-base"),g(M,"class","section-title"),g(P,"class","table-compact table-border m-b-base"),g(ae,"class","section-title"),g(se,"class","tabs-header compact combined left"),g(oe,"class","tabs-content"),g(G,"class","tabs")},m(c,p){_(c,t,p),n(t,e),n(t,l),n(t,h),n(h,i),n(i,r),n(i,o),n(o,v),n(i,C),_(c,F,p),_(c,B,p),_(c,I,p),_(c,D,p),_(c,z,p),_(c,M,p),_(c,U,p),_(c,P,p),n(P,y),n(P,q),n(P,$),n($,L),n(L,Y),n(L,A),n(L,K),n(L,j),n(L,a),n(a,k),ee(O,a,null),n(a,Z),n(a,u),n(a,f),n(a,S),n(a,w),n(a,X),n(a,we),n(a,Te),n(a,Oe),n(a,fe),n(a,ye),n($,Pe),ee(le,$,null),_(c,pe,p),_(c,ae,p),_(c,me,p),_(c,G,p),n(G,se);for(let W=0;W<Q.length;W+=1)Q[W]&&Q[W].m(se,null);n(G,qe),n(G,oe);for(let W=0;W<H.length;W+=1)H[W]&&H[W].m(oe,null);ne=!0},p(c,[p]){(!ne||p&1)&&m!==(m=c[0].name+"")&&re(v,m),p&6&&(ce=V(c[2]),Q=de(Q,p,Ae,1,c,ce,Se,se,Ee,We,null,Ie)),p&6&&(ie=V(c[2]),ue(),H=de(H,p,Re,1,c,ie,Ce,oe,ze,Fe,null,Be),he())},i(c){if(!ne){J(O.$$.fragment,c),J(le.$$.fragment,c);for(let p=0;p<ie.length;p+=1)J(H[p]);ne=!0}},o(c){E(O.$$.fragment,c),E(le.$$.fragment,c);for(let p=0;p<H.length;p+=1)E(H[p]);ne=!1},d(c){c&&(b(t),b(F),b(B),b(I),b(D),b(z),b(M),b(U),b(P),b(pe),b(ae),b(me),b(G)),x(O),x(le);for(let p=0;p<Q.length;p+=1)Q[p].d();for(let p=0;p<H.length;p+=1)H[p].d()}}}function Ge(s,t,e){let{collection:l}=t,h=200,i=[];const r=o=>e(1,h=o.code);return s.$$set=o=>{"collection"in o&&e(0,l=o.collection)},s.$$.update=()=>{s.$$.dirty&1&&e(2,i=[{code:200,body:JSON.stringify({token:"JWT_TOKEN",record:$e.dummyCollectionRecord(l)},null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "Failed to authenticate.",
                  "data": {
                    "otpId": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `}])},[l,h,i,r]}class xe extends be{constructor(t){super(),_e(this,t,Ge,Ze,ve,{collection:0})}}function Ue(s,t,e){const l=s.slice();return l[4]=t[e],l}function He(s,t,e){const l=s.slice();return l[4]=t[e],l}function Le(s,t){let e,l=t[4].code+"",h,i,r,o;function m(){return t[3](t[4])}return{key:s,first:null,c(){e=d("button"),h=R(l),i=T(),g(e,"class","tab-item"),N(e,"active",t[1]===t[4].code),this.first=e},m(v,C){_(v,e,C),n(e,h),n(e,i),r||(o=ke(e,"click",m),r=!0)},p(v,C){t=v,C&4&&l!==(l=t[4].code+"")&&re(h,l),C&6&&N(e,"active",t[1]===t[4].code)},d(v){v&&b(e),r=!1,o()}}}function Ye(s,t){let e,l,h,i;return l=new ge({props:{content:t[4].body}}),{key:s,first:null,c(){e=d("div"),te(l.$$.fragment),h=T(),g(e,"class","tab-item"),N(e,"active",t[1]===t[4].code),this.first=e},m(r,o){_(r,e,o),ee(l,e,null),n(e,h),i=!0},p(r,o){t=r;const m={};o&4&&(m.content=t[4].body),l.$set(m),(!i||o&6)&&N(e,"active",t[1]===t[4].code)},i(r){i||(J(l.$$.fragment,r),i=!0)},o(r){E(l.$$.fragment,r),i=!1},d(r){r&&b(e),x(l)}}}function et(s){let t,e,l,h,i,r,o,m=s[0].name+"",v,C,F,B,I,D,z,M,U,P,y,q=[],$=new Map,L,Y,A=[],K=new Map,j,a=V(s[2]);const k=u=>u[4].code;for(let u=0;u<a.length;u+=1){let f=He(s,a,u),S=k(f);$.set(S,q[u]=Le(S,f))}let O=V(s[2]);const Z=u=>u[4].code;for(let u=0;u<O.length;u+=1){let f=Ue(s,O,u),S=Z(f);K.set(S,A[u]=Ye(S,f))}return{c(){t=d("div"),e=d("strong"),e.textContent="POST",l=T(),h=d("div"),i=d("p"),r=R("/api/collections/"),o=d("strong"),v=R(m),C=R("/request-otp"),F=T(),B=d("div"),B.textContent="Body Parameters",I=T(),D=d("table"),D.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>email</span></div></td> <td><span class="label">String</span></td> <td>The auth record email address to send the OTP request (if exists).</td></tr></tbody>',z=T(),M=d("div"),M.textContent="Responses",U=T(),P=d("div"),y=d("div");for(let u=0;u<q.length;u+=1)q[u].c();L=T(),Y=d("div");for(let u=0;u<A.length;u+=1)A[u].c();g(e,"class","label label-primary"),g(h,"class","content"),g(t,"class","alert alert-success"),g(B,"class","section-title"),g(D,"class","table-compact table-border m-b-base"),g(M,"class","section-title"),g(y,"class","tabs-header compact combined left"),g(Y,"class","tabs-content"),g(P,"class","tabs")},m(u,f){_(u,t,f),n(t,e),n(t,l),n(t,h),n(h,i),n(i,r),n(i,o),n(o,v),n(i,C),_(u,F,f),_(u,B,f),_(u,I,f),_(u,D,f),_(u,z,f),_(u,M,f),_(u,U,f),_(u,P,f),n(P,y);for(let S=0;S<q.length;S+=1)q[S]&&q[S].m(y,null);n(P,L),n(P,Y);for(let S=0;S<A.length;S+=1)A[S]&&A[S].m(Y,null);j=!0},p(u,[f]){(!j||f&1)&&m!==(m=u[0].name+"")&&re(v,m),f&6&&(a=V(u[2]),q=de(q,f,k,1,u,a,$,y,Ee,Le,null,He)),f&6&&(O=V(u[2]),ue(),A=de(A,f,Z,1,u,O,K,Y,ze,Ye,null,Ue),he())},i(u){if(!j){for(let f=0;f<O.length;f+=1)J(A[f]);j=!0}},o(u){for(let f=0;f<A.length;f+=1)E(A[f]);j=!1},d(u){u&&(b(t),b(F),b(B),b(I),b(D),b(z),b(M),b(U),b(P));for(let f=0;f<q.length;f+=1)q[f].d();for(let f=0;f<A.length;f+=1)A[f].d()}}}function tt(s,t,e){let{collection:l}=t,h=200,i=[];const r=o=>e(1,h=o.code);return s.$$set=o=>{"collection"in o&&e(0,l=o.collection)},e(2,i=[{code:200,body:JSON.stringify({otpId:$e.randomString(15)},null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "An error occurred while validating the submitted data.",
                  "data": {
                    "email": {
                      "code": "validation_is_email",
                      "message": "Must be a valid email address."
                    }
                  }
                }
            `},{code:429,body:`
                {
                  "status": 429,
                  "message": "You've send too many OTP requests, please try again later.",
                  "data": {}
                }
            `}]),[l,h,i,r]}class lt extends be{constructor(t){super(),_e(this,t,tt,et,ve,{collection:0})}}function Ve(s,t,e){const l=s.slice();return l[5]=t[e],l[7]=e,l}function Je(s,t,e){const l=s.slice();return l[5]=t[e],l[7]=e,l}function Ne(s){let t,e,l,h,i;function r(){return s[4](s[7])}return{c(){t=d("button"),e=d("div"),e.textContent=`${s[5].title}`,l=T(),g(e,"class","txt"),g(t,"class","tab-item"),N(t,"active",s[1]==s[7])},m(o,m){_(o,t,m),n(t,e),n(t,l),h||(i=ke(t,"click",r),h=!0)},p(o,m){s=o,m&2&&N(t,"active",s[1]==s[7])},d(o){o&&b(t),h=!1,i()}}}function je(s){let t,e,l,h;var i=s[5].component;function r(o,m){return{props:{collection:o[0]}}}return i&&(e=Me(i,r(s))),{c(){t=d("div"),e&&te(e.$$.fragment),l=T(),g(t,"class","tab-item"),N(t,"active",s[1]==s[7])},m(o,m){_(o,t,m),e&&ee(e,t,null),n(t,l),h=!0},p(o,m){if(i!==(i=o[5].component)){if(e){ue();const v=e;E(v.$$.fragment,1,0,()=>{x(v,1)}),he()}i?(e=Me(i,r(o)),te(e.$$.fragment),J(e.$$.fragment,1),ee(e,t,l)):e=null}else if(i){const v={};m&1&&(v.collection=o[0]),e.$set(v)}(!h||m&2)&&N(t,"active",o[1]==o[7])},i(o){h||(e&&J(e.$$.fragment,o),h=!0)},o(o){e&&E(e.$$.fragment,o),h=!1},d(o){o&&b(t),e&&x(e)}}}function at(s){var Y,A,K,j;let t,e,l=s[0].name+"",h,i,r,o,m,v,C,F,B,I,D,z,M,U;v=new Qe({props:{js:`
        import Base from 'base';

        const base = new Base('${s[2]}');

        ...

        // send OTP email to the provided auth record
        const req = await base.collection('${(Y=s[0])==null?void 0:Y.name}').requestOTP('test@example.com');

        // ... show a screen/popup to enter the password from the email ...

        // authenticate with the requested OTP id and the email password
        const authData = await base.collection('${(A=s[0])==null?void 0:A.name}').authWithOTP(
            req.otpId,
            "YOUR_OTP",
        );

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${s[2]}');

        ...

        // send OTP email to the provided auth record
        final req = await base.collection('${(K=s[0])==null?void 0:K.name}').requestOTP('test@example.com');

        // ... show a screen/popup to enter the password from the email ...

        // authenticate with the requested OTP id and the email password
        final authData = await base.collection('${(j=s[0])==null?void 0:j.name}').authWithOTP(
            req.otpId,
            "YOUR_OTP",
        );

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `}});let P=V(s[3]),y=[];for(let a=0;a<P.length;a+=1)y[a]=Ne(Je(s,P,a));let q=V(s[3]),$=[];for(let a=0;a<q.length;a+=1)$[a]=je(Ve(s,q,a));const L=a=>E($[a],1,1,()=>{$[a]=null});return{c(){t=d("h3"),e=R("Auth with OTP ("),h=R(l),i=R(")"),r=T(),o=d("div"),o.innerHTML=`<p>Authenticate with an one-time password (OTP).</p> <p>Note that when requesting an OTP we return an <code>otpId</code> even if a user with the provided email
        doesn&#39;t exist as a very basic enumeration protection.</p>`,m=T(),te(v.$$.fragment),C=T(),F=d("h6"),F.textContent="API details",B=T(),I=d("div"),D=d("div");for(let a=0;a<y.length;a+=1)y[a].c();z=T(),M=d("div");for(let a=0;a<$.length;a+=1)$[a].c();g(t,"class","m-b-sm"),g(o,"class","content txt-lg m-b-sm"),g(F,"class","m-b-xs"),g(D,"class","tabs-header compact"),g(M,"class","tabs-content"),g(I,"class","tabs")},m(a,k){_(a,t,k),n(t,e),n(t,h),n(t,i),_(a,r,k),_(a,o,k),_(a,m,k),ee(v,a,k),_(a,C,k),_(a,F,k),_(a,B,k),_(a,I,k),n(I,D);for(let O=0;O<y.length;O+=1)y[O]&&y[O].m(D,null);n(I,z),n(I,M);for(let O=0;O<$.length;O+=1)$[O]&&$[O].m(M,null);U=!0},p(a,[k]){var Z,u,f,S;(!U||k&1)&&l!==(l=a[0].name+"")&&re(h,l);const O={};if(k&5&&(O.js=`
        import Base from 'base';

        const base = new Base('${a[2]}');

        ...

        // send OTP email to the provided auth record
        const req = await base.collection('${(Z=a[0])==null?void 0:Z.name}').requestOTP('test@example.com');

        // ... show a screen/popup to enter the password from the email ...

        // authenticate with the requested OTP id and the email password
        const authData = await base.collection('${(u=a[0])==null?void 0:u.name}').authWithOTP(
            req.otpId,
            "YOUR_OTP",
        );

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),k&5&&(O.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${a[2]}');

        ...

        // send OTP email to the provided auth record
        final req = await base.collection('${(f=a[0])==null?void 0:f.name}').requestOTP('test@example.com');

        // ... show a screen/popup to enter the password from the email ...

        // authenticate with the requested OTP id and the email password
        final authData = await base.collection('${(S=a[0])==null?void 0:S.name}').authWithOTP(
            req.otpId,
            "YOUR_OTP",
        );

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),v.$set(O),k&10){P=V(a[3]);let w;for(w=0;w<P.length;w+=1){const X=Je(a,P,w);y[w]?y[w].p(X,k):(y[w]=Ne(X),y[w].c(),y[w].m(D,null))}for(;w<y.length;w+=1)y[w].d(1);y.length=P.length}if(k&11){q=V(a[3]);let w;for(w=0;w<q.length;w+=1){const X=Ve(a,q,w);$[w]?($[w].p(X,k),J($[w],1)):($[w]=je(X),$[w].c(),J($[w],1),$[w].m(M,null))}for(ue(),w=q.length;w<$.length;w+=1)L(w);he()}},i(a){if(!U){J(v.$$.fragment,a);for(let k=0;k<q.length;k+=1)J($[k]);U=!0}},o(a){E(v.$$.fragment,a),$=$.filter(Boolean);for(let k=0;k<$.length;k+=1)E($[k]);U=!1},d(a){a&&(b(t),b(r),b(o),b(m),b(C),b(F),b(B),b(I)),x(v,a),De(y,a),De($,a)}}}function st(s,t,e){let l,{collection:h}=t;const i=[{title:"OTP Request",component:lt},{title:"OTP Auth",component:xe}];let r=0;const o=m=>e(1,r=m);return s.$$set=m=>{"collection"in m&&e(0,h=m.collection)},e(2,l=$e.getApiExampleUrl(Ke.baseURL)),[h,r,l,i,o]}class it extends be{constructor(t){super(),_e(this,t,st,at,ve,{collection:0})}}export{it as default};
