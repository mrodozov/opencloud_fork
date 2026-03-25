// Package inference provides CGo bindings to the Rockchip RKNN Runtime library
// (librknnrt.so) for running neural network inference on the RK3566 NPU
// found in the Odroid-M1S board.
//
// Prerequisites on the target device:
//   - Install the RKNN Toolkit2 runtime: https://github.com/rockchip-linux/rknn-toolkit2
//   - librknnrt.so must be on the library path (typically /usr/lib or /usr/local/lib)
//   - The .rknn model must be converted with rknn-toolkit2 targeting rk3566
package inference

/*
#cgo LDFLAGS: -lrknnrt
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef uint64_t rknn_context;

// Return codes
#define RKNN_SUCC                    0
#define RKNN_ERR_FAIL               -1
#define RKNN_ERR_TIMEOUT            -2
#define RKNN_ERR_DEVICE_UNAVAILABLE -3
#define RKNN_ERR_MALLOC_FAIL        -4
#define RKNN_ERR_PARAM_INVALID      -5
#define RKNN_ERR_MODEL_INVALID      -6
#define RKNN_ERR_CTX_INVALID        -7
#define RKNN_ERR_INPUT_INVALID      -8
#define RKNN_ERR_OUTPUT_INVALID     -9

// Tensor data types
typedef enum {
    RKNN_TENSOR_FLOAT32   = 0,
    RKNN_TENSOR_FLOAT16   = 1,
    RKNN_TENSOR_INT8      = 2,
    RKNN_TENSOR_UINT8     = 3,
    RKNN_TENSOR_INT16     = 4,
    RKNN_TENSOR_UINT16    = 5,
    RKNN_TENSOR_INT32     = 6,
    RKNN_TENSOR_UINT32    = 7,
    RKNN_TENSOR_INT64     = 8,
    RKNN_TENSOR_BOOL      = 9,
    RKNN_TENSOR_TYPE_MAX  = 10,
} rknn_tensor_type;

// Tensor memory layout
typedef enum {
    RKNN_TENSOR_NCHW      = 0,
    RKNN_TENSOR_NHWC      = 1,
    RKNN_TENSOR_NC1HWC2   = 2,
    RKNN_TENSOR_UNDEFINED = 3,
} rknn_tensor_format;

// Input descriptor passed to rknn_inputs_set
typedef struct {
    uint32_t           index;        // tensor index (0-based)
    void*              buf;          // pointer to input data
    uint32_t           size;         // byte size of buf
    uint8_t            pass_through; // 1 = raw quantised data, skip normalisation
    rknn_tensor_type   type;         // data type of buf
    rknn_tensor_format fmt;          // memory layout of buf
} rknn_input;

// Output descriptor passed to rknn_outputs_get
typedef struct {
    uint8_t  want_float;   // 1 = dequantise output to float32
    uint8_t  is_prealloc;  // 1 = buf is caller-allocated
    uint32_t index;        // tensor index (0-based)
    void*    buf;          // pointer to output data (or NULL when is_prealloc=0)
    uint32_t size;         // byte size of buf (filled by runtime when is_prealloc=0)
} rknn_output;

// Optional extension struct for rknn_init — pass NULL for default behaviour
typedef struct {
    void* reserved;
} rknn_init_extend;

int rknn_init(rknn_context* context, void* model, uint32_t size,
              uint32_t flag, rknn_init_extend* extend);
int rknn_inputs_set(rknn_context context, uint32_t n_inputs,
                    rknn_input inputs[]);
int rknn_run(rknn_context context, void* extend);
int rknn_outputs_get(rknn_context context, uint32_t n_outputs,
                     rknn_output outputs[], void* extend);
int rknn_outputs_release(rknn_context context, uint32_t n_outputs,
                         rknn_output outputs[]);
int rknn_destroy(rknn_context context);
*/
import "C"

import (
	"fmt"
)

// Context wraps an rknn_context handle and is not safe for concurrent use.
// Create one Context per goroutine, or serialise access with a mutex.
type Context struct {
	ctx C.rknn_context
}

// Init loads an RKNN model from model bytes and initialises an NPU context.
// The caller must call Destroy when finished.
func Init(modelData []byte) (*Context, error) {
	if len(modelData) == 0 {
		return nil, fmt.Errorf("rknn: empty model data")
	}
	cModel := C.CBytes(modelData)
        defer C.free(cModel)
	
	var ctx C.rknn_context
	ret := C.rknn_init(
		&ctx,
		cModel,
		C.uint32_t(len(modelData)),
		0,
		nil,
	)
	if ret != C.RKNN_SUCC {
		return nil, fmt.Errorf("rknn_init failed: error code %d", int(ret))
	}
	return &Context{ctx: ctx}, nil
}

// RunUint8 submits a single uint8 NHWC image tensor to the NPU and returns
// the raw float32 output buffer of the first output tensor.
//
// imgData must be exactly inputW * inputH * 3 bytes in NHWC (H,W,C=RGB) order.
func (c *Context) RunUint8(imgData []byte, inputW, inputH int) ([]float32, error) {
	expectedSize := inputW * inputH * 3
	if len(imgData) != expectedSize {
		return nil, fmt.Errorf("rknn: expected %d bytes (NHWC %dx%dx3), got %d",
			expectedSize, inputH, inputW, len(imgData))
	}

	// --- set input ---
	cBuf := C.CBytes(imgData)
        defer C.free(cBuf)

	input := C.rknn_input{
		index:        0,
		buf:          cBuf,
		size:         C.uint32_t(len(imgData)),
		pass_through: 0,                    // let RKNN apply normalisation configured at conversion time
		_type:        C.RKNN_TENSOR_UINT8,
		fmt:          C.RKNN_TENSOR_NHWC,
	}
	if ret := C.rknn_inputs_set(c.ctx, 1, &input); ret != C.RKNN_SUCC {
		return nil, fmt.Errorf("rknn_inputs_set failed: %d", int(ret))
	}

	// --- run inference ---
	if ret := C.rknn_run(c.ctx, nil); ret != C.RKNN_SUCC {
		return nil, fmt.Errorf("rknn_run failed: %d", int(ret))
	}

	// --- retrieve output (request float32 dequantisation) ---
	output := C.rknn_output{
		want_float:  1,
		is_prealloc: 0,
		index:       0,
	}
	if ret := C.rknn_outputs_get(c.ctx, 1, &output, nil); ret != C.RKNN_SUCC {
		return nil, fmt.Errorf("rknn_outputs_get failed: %d", int(ret))
	}
	defer C.rknn_outputs_release(c.ctx, 1, &output)

	// copy output into a Go slice before the deferred release
	numFloats := int(output.size) / 4 // float32 = 4 bytes
	if numFloats == 0 {
		return nil, fmt.Errorf("rknn: output tensor is empty")
	}
	out := make([]float32, numFloats)
	cSlice := (*[1 << 28]C.float)(output.buf)[:numFloats:numFloats]
	for i, v := range cSlice {
		out[i] = float32(v)
	}
	return out, nil
}

// Destroy releases the NPU context and all associated resources.
func (c *Context) Destroy() {
	if c != nil {
		C.rknn_destroy(c.ctx)
	}
}
