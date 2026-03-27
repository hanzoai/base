import{S as Je,i as xe,s as ze,V as Ee,W as Ve,X as I,j as r,d as Q,t as V,a as J,I as pe,Z as Be,_ as Ne,C as Ie,$ as Qe,D as Ze,n as c,o as a,m as Z,u as n,A as _,v as h,c as K,w as p,J as Fe,b as Ke,l as X,p as Xe}from"./index-CzlWNNWT.js";import{F as Ge}from"./FieldsQueryParam-BjMsFtDw.js";function Le(o,l,s){const i=o.slice();return i[5]=l[s],i}function je(o,l,s){const i=o.slice();return i[5]=l[s],i}function He(o,l){let s,i=l[5].code+"",f,g,d,b;function v(){return l[4](l[5])}return{key:o,first:null,c(){s=n("button"),f=_(i),g=h(),p(s,"class","tab-item"),X(s,"active",l[1]===l[5].code),this.first=s},m(k,O){c(k,s,O),a(s,f),a(s,g),d||(b=Xe(s,"click",v),d=!0)},p(k,O){l=k,O&4&&i!==(i=l[5].code+"")&&pe(f,i),O&6&&X(s,"active",l[1]===l[5].code)},d(k){k&&r(s),d=!1,b()}}}function Pe(o,l){let s,i,f,g;return i=new Ve({props:{content:l[5].body}}),{key:o,first:null,c(){s=n("div"),K(i.$$.fragment),f=h(),p(s,"class","tab-item"),X(s,"active",l[1]===l[5].code),this.first=s},m(d,b){c(d,s,b),Z(i,s,null),a(s,f),g=!0},p(d,b){l=d;const v={};b&4&&(v.content=l[5].body),i.$set(v),(!g||b&6)&&X(s,"active",l[1]===l[5].code)},i(d){g||(J(i.$$.fragment,d),g=!0)},o(d){V(i.$$.fragment,d),g=!1},d(d){d&&r(s),Q(i)}}}function Ye(o){let l,s,i=o[0].name+"",f,g,d,b,v,k,O,R,G,A,x,be,z,M,me,Y,E=o[0].name+"",ee,fe,te,W,ae,U,le,B,se,y,ne,ge,F,S,oe,_e,ie,ve,m,ke,C,we,$e,Oe,re,Ae,ce,ye,Se,Te,de,Ce,qe,q,ue,L,he,T,j,$=[],De=new Map,Re,H,w=[],Me=new Map,D;k=new Ee({props:{js:`
        import Base from 'base';

        const base = new Base('${o[3]}');

        ...

        // OAuth2 authentication with a single realtime call.
        //
        // Make sure to register ${o[3]}/api/oauth2-redirect as redirect url.
        const authData = await base.collection('${o[0].name}').authWithOAuth2({ provider: 'google' });

        // OR authenticate with manual OAuth2 code exchange
        // const authData = await base.collection('${o[0].name}').authWithOAuth2Code(...);

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `,dart:`
        import 'package:hanzoai/base.dart';
        import 'package:url_launcher/url_launcher.dart';

        final base = Base('${o[3]}');

        ...

        // OAuth2 authentication with a single realtime call.
        //
        // Make sure to register ${o[3]}/api/oauth2-redirect as redirect url.
        final authData = await base.collection('${o[0].name}').authWithOAuth2('google', (url) async {
          await launchUrl(url);
        });

        // OR authenticate with manual OAuth2 code exchange
        // final authData = await base.collection('${o[0].name}').authWithOAuth2Code(...);

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `}}),C=new Ve({props:{content:"?expand=relField1,relField2.subRelField"}}),q=new Ge({props:{prefix:"record."}});let N=I(o[2]);const We=e=>e[5].code;for(let e=0;e<N.length;e+=1){let t=je(o,N,e),u=We(t);De.set(u,$[e]=He(u,t))}let P=I(o[2]);const Ue=e=>e[5].code;for(let e=0;e<P.length;e+=1){let t=Le(o,P,e),u=Ue(t);Me.set(u,w[e]=Pe(u,t))}return{c(){l=n("h3"),s=_("Auth with OAuth2 ("),f=_(i),g=_(")"),d=h(),b=n("div"),b.innerHTML=`<p>Authenticate with an OAuth2 provider and returns a new auth token and record data.</p> <p>For more details please check the
        <a href="undefined" target="_blank" rel="noopener noreferrer">OAuth2 integration documentation
        </a>.</p>`,v=h(),K(k.$$.fragment),O=h(),R=n("h6"),R.textContent="API details",G=h(),A=n("div"),x=n("strong"),x.textContent="POST",be=h(),z=n("div"),M=n("p"),me=_("/api/collections/"),Y=n("strong"),ee=_(E),fe=_("/auth-with-oauth2"),te=h(),W=n("div"),W.textContent="Body Parameters",ae=h(),U=n("table"),U.innerHTML=`<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>provider</span></div></td> <td><span class="label">String</span></td> <td>The name of the OAuth2 client provider (eg. &quot;google&quot;).</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>code</span></div></td> <td><span class="label">String</span></td> <td>The authorization code returned from the initial request.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>codeVerifier</span></div></td> <td><span class="label">String</span></td> <td>The code verifier sent with the initial request as part of the code_challenge.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>redirectURL</span></div></td> <td><span class="label">String</span></td> <td>The redirect url sent with the initial request.</td></tr> <tr><td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>createData</span></div></td> <td><span class="label">Object</span></td> <td><p>Optional data that will be used when creating the auth record on OAuth2 sign-up.</p> <p>The created auth record must comply with the same requirements and validations in the
                    regular <strong>create</strong> action.
                    <br/> <em>The data can only be in <code>json</code>, aka. <code>multipart/form-data</code> and files
                        upload currently are not supported during OAuth2 sign-ups.</em></p></td></tr></tbody>`,le=h(),B=n("div"),B.textContent="Query parameters",se=h(),y=n("table"),ne=n("thead"),ne.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',ge=h(),F=n("tbody"),S=n("tr"),oe=n("td"),oe.textContent="expand",_e=h(),ie=n("td"),ie.innerHTML='<span class="label">String</span>',ve=h(),m=n("td"),ke=_(`Auto expand record relations. Ex.:
                `),K(C.$$.fragment),we=_(`
                Supports up to 6-levels depth nested relations expansion. `),$e=n("br"),Oe=_(`
                The expanded relations will be appended to the record under the
                `),re=n("code"),re.textContent="expand",Ae=_(" property (eg. "),ce=n("code"),ce.textContent='"expand": {"relField1": {...}, ...}',ye=_(`).
                `),Se=n("br"),Te=_(`
                Only the relations to which the request user has permissions to `),de=n("strong"),de.textContent="view",Ce=_(" will be expanded."),qe=h(),K(q.$$.fragment),ue=h(),L=n("div"),L.textContent="Responses",he=h(),T=n("div"),j=n("div");for(let e=0;e<$.length;e+=1)$[e].c();Re=h(),H=n("div");for(let e=0;e<w.length;e+=1)w[e].c();p(l,"class","m-b-sm"),p(b,"class","content txt-lg m-b-sm"),p(R,"class","m-b-xs"),p(x,"class","label label-primary"),p(z,"class","content"),p(A,"class","alert alert-success"),p(W,"class","section-title"),p(U,"class","table-compact table-border m-b-base"),p(B,"class","section-title"),p(y,"class","table-compact table-border m-b-base"),p(L,"class","section-title"),p(j,"class","tabs-header compact combined left"),p(H,"class","tabs-content"),p(T,"class","tabs")},m(e,t){c(e,l,t),a(l,s),a(l,f),a(l,g),c(e,d,t),c(e,b,t),c(e,v,t),Z(k,e,t),c(e,O,t),c(e,R,t),c(e,G,t),c(e,A,t),a(A,x),a(A,be),a(A,z),a(z,M),a(M,me),a(M,Y),a(Y,ee),a(M,fe),c(e,te,t),c(e,W,t),c(e,ae,t),c(e,U,t),c(e,le,t),c(e,B,t),c(e,se,t),c(e,y,t),a(y,ne),a(y,ge),a(y,F),a(F,S),a(S,oe),a(S,_e),a(S,ie),a(S,ve),a(S,m),a(m,ke),Z(C,m,null),a(m,we),a(m,$e),a(m,Oe),a(m,re),a(m,Ae),a(m,ce),a(m,ye),a(m,Se),a(m,Te),a(m,de),a(m,Ce),a(F,qe),Z(q,F,null),c(e,ue,t),c(e,L,t),c(e,he,t),c(e,T,t),a(T,j);for(let u=0;u<$.length;u+=1)$[u]&&$[u].m(j,null);a(T,Re),a(T,H);for(let u=0;u<w.length;u+=1)w[u]&&w[u].m(H,null);D=!0},p(e,[t]){(!D||t&1)&&i!==(i=e[0].name+"")&&pe(f,i);const u={};t&9&&(u.js=`
        import Base from 'base';

        const base = new Base('${e[3]}');

        ...

        // OAuth2 authentication with a single realtime call.
        //
        // Make sure to register ${e[3]}/api/oauth2-redirect as redirect url.
        const authData = await base.collection('${e[0].name}').authWithOAuth2({ provider: 'google' });

        // OR authenticate with manual OAuth2 code exchange
        // const authData = await base.collection('${e[0].name}').authWithOAuth2Code(...);

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),t&9&&(u.dart=`
        import 'package:hanzoai/base.dart';
        import 'package:url_launcher/url_launcher.dart';

        final base = Base('${e[3]}');

        ...

        // OAuth2 authentication with a single realtime call.
        //
        // Make sure to register ${e[3]}/api/oauth2-redirect as redirect url.
        final authData = await base.collection('${e[0].name}').authWithOAuth2('google', (url) async {
          await launchUrl(url);
        });

        // OR authenticate with manual OAuth2 code exchange
        // final authData = await base.collection('${e[0].name}').authWithOAuth2Code(...);

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),k.$set(u),(!D||t&1)&&E!==(E=e[0].name+"")&&pe(ee,E),t&6&&(N=I(e[2]),$=Be($,t,We,1,e,N,De,j,Ne,He,null,je)),t&6&&(P=I(e[2]),Ie(),w=Be(w,t,Ue,1,e,P,Me,H,Qe,Pe,null,Le),Ze())},i(e){if(!D){J(k.$$.fragment,e),J(C.$$.fragment,e),J(q.$$.fragment,e);for(let t=0;t<P.length;t+=1)J(w[t]);D=!0}},o(e){V(k.$$.fragment,e),V(C.$$.fragment,e),V(q.$$.fragment,e);for(let t=0;t<w.length;t+=1)V(w[t]);D=!1},d(e){e&&(r(l),r(d),r(b),r(v),r(O),r(R),r(G),r(A),r(te),r(W),r(ae),r(U),r(le),r(B),r(se),r(y),r(ue),r(L),r(he),r(T)),Q(k,e),Q(C),Q(q);for(let t=0;t<$.length;t+=1)$[t].d();for(let t=0;t<w.length;t+=1)w[t].d()}}}function et(o,l,s){let i,{collection:f}=l,g=200,d=[];const b=v=>s(1,g=v.code);return o.$$set=v=>{"collection"in v&&s(0,f=v.collection)},o.$$.update=()=>{o.$$.dirty&1&&s(2,d=[{code:200,body:JSON.stringify({token:"JWT_AUTH_TOKEN",record:Fe.dummyCollectionRecord(f),meta:{id:"abc123",name:"John Doe",username:"john.doe",email:"test@example.com",avatarURL:"https://example.com/avatar.png",accessToken:"...",refreshToken:"...",expiry:"2022-01-01 10:00:00.123Z",isNew:!1,rawUser:{}}},null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "An error occurred while submitting the form.",
                  "data": {
                    "provider": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `}])},s(3,i=Fe.getApiExampleUrl(Ke.baseURL)),[f,g,d,i,b]}class lt extends Je{constructor(l){super(),xe(this,l,et,Ye,ze,{collection:0})}}export{lt as default};
