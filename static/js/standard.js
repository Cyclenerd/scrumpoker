// Based on standard_HD.glsl by KF2015 (https://www.youtube.com/watch?v=RQ_lf3LwURE)

function initShader() {
    const canvas = document.getElementById('shaderCanvas');
    if (!canvas) return;
    const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
    if (!gl) return;

    const vertexShaderSource = `
                attribute vec2 position;
                void main() {
                    gl_Position = vec4(position, 0.0, 1.0);
                }
            `;

    const fragmentShaderSource = `
                precision mediump float;
                uniform vec2 iResolution;
                uniform float iTime;

                #define SPIN_EASE 0.5
                #define colour_1 vec4(254./255.,95./255.,85./255.,1.0)*vec4(0.85)
                #define colour_2 vec4(55./255.,66./255.,68./250.,1.0)
                #define colour_3 vec4(0.0,0.0,0.0,1.0)
                #define contrast 3.0
                #define spin_amount 0.0
                #define spin_time (iTime*spin_amount)

                void main() {
                    vec2 fragCoord = gl_FragCoord.xy;
                    float pixel_size = 1.0;
                    vec2 uv = (floor((fragCoord.xy)*(1./pixel_size))*pixel_size - 0.5*iResolution.xy)/length(iResolution.xy);
                    float uv_len = length(uv);

                    float speed = (spin_time*SPIN_EASE*0.2) + 302.2;
                    float new_pixel_angle = (atan(uv.y, uv.x)) + speed - SPIN_EASE*20.*(1.*spin_amount*uv_len + (1. - 1.*spin_amount));
                    uv = vec2(uv_len * cos(new_pixel_angle), uv_len * sin(new_pixel_angle));

                    uv *= 30.;
                    speed = iTime*(2.);
                    vec2 uv2 = vec2(uv.x+uv.y);

                    for(int i=0; i < 5; i++) {
                        uv2 += sin(max(uv.x, uv.y)) + uv;
                        uv  += 0.5*vec2(cos(5.1123314 + 0.353*uv2.y + speed*0.131121),sin(uv2.x - 0.113*speed));
                        uv  -= 1.0*cos(uv.x + uv.y) - 1.0*sin(uv.x*0.711 - uv.y);
                    }

                    float contrast_mod = (0.25*contrast + 0.5*spin_amount + 1.2);
                    float paint_res =min(2., max(0.,length(uv)*(0.035)*contrast_mod));

                    float c1p = max(0.,1. - contrast_mod*abs(1.-paint_res));
                    float c2p = max(0.,1. - contrast_mod*abs(paint_res));
                    float c3p = 1. - min(1., c1p + c2p);
                    
                    float shine = 0.*(0.3*max(c1p*5. - 4., 0.) + 0.4*max(c2p*5. - 4., 0.));

                    vec4 ret_col = (0.3/contrast)*colour_1 + (1. - 0.3/contrast)*(colour_1*c1p + colour_2*c2p + vec4(c3p*colour_3.rgb, c3p*colour_1.a)) + shine;

                    gl_FragColor = ret_col;
                }
            `;

    function createShader(gl, type, source) {
        const shader = gl.createShader(type);
        gl.shaderSource(shader, source);
        gl.compileShader(shader);
        if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
            console.error(gl.getShaderInfoLog(shader));
            gl.deleteShader(shader);
            return null;
        }
        return shader;
    }

    const vertexShader = createShader(gl, gl.VERTEX_SHADER, vertexShaderSource);
    const fragmentShader = createShader(gl, gl.FRAGMENT_SHADER, fragmentShaderSource);

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);

    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
        console.error(gl.getProgramInfoLog(program));
        return;
    }

    const positionBuffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([
        -1, -1,
        1, -1,
        -1, 1,
        1, 1
    ]), gl.STATIC_DRAW);

    const positionLocation = gl.getAttribLocation(program, 'position');
    const iResolutionLocation = gl.getUniformLocation(program, 'iResolution');
    const iTimeLocation = gl.getUniformLocation(program, 'iTime');

    function resize() {
        canvas.width = window.innerWidth;
        canvas.height = window.innerHeight;
        gl.viewport(0, 0, canvas.width, canvas.height);
    }
    resize();
    window.addEventListener('resize', resize);

    const startTime = Date.now();

    function render() {
        const time = (Date.now() - startTime) / 1000;

        gl.clearColor(0, 0, 0, 1);
        gl.clear(gl.COLOR_BUFFER_BIT);

        gl.useProgram(program);

        gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
        gl.enableVertexAttribArray(positionLocation);
        gl.vertexAttribPointer(positionLocation, 2, gl.FLOAT, false, 0, 0);

        gl.uniform2f(iResolutionLocation, canvas.width, canvas.height);
        gl.uniform1f(iTimeLocation, time);

        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);

        requestAnimationFrame(render);
    }
    render();
}
