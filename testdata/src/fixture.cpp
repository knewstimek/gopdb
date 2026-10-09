// Test fixture for the PDB reader. Built without the C runtime (see
// build.py), so it only uses language features that need no library code.

// Floating point code references this CRT marker; no CRT is linked.
extern "C" int _fltused = 0;

typedef unsigned int Handle;

enum Color { Red = 1, Green = 2, Blue = 40000 };

enum class Big : long long { Low = -5, High = 0x123456789 };

struct Point {
	int x;
	int y;
};

struct Flags {
	unsigned int ready : 1;
	unsigned int mode : 3;
	unsigned int rest : 28;
};

union Value {
	int i;
	float f;
	char bytes[4];
};

struct Node {
	Node *next;
	Point pos[3];
	Value value;
	Flags flags;
	Color color;
	const char *name;
	int (*callback)(int, Point *);
	double weight;
};

namespace geo {
class Shape {
public:
	Shape(int id) : id_(id) {}
	virtual int area() const { return 0; }
	int id() const { return id_; }
	static int count;

private:
	int id_;
};

class Rect : public Shape {
public:
	Rect(int id, int w, int h) : Shape(id), w_(w), h_(h) {}
	int area() const override { return w_ * h_; }

private:
	int w_, h_;
};

int Shape::count = 0;
} // namespace geo

template <typename T> struct Box {
	T item;
	T get() const { return item; }
};

Node g_nodes[4];
Handle g_handle = 7;
static int s_counter;
geo::Rect *g_rect;

__declspec(noreturn) void fatal(int code) {
	for (;;) {
		s_counter += code;
	}
}

__declspec(noinline) int sum_points(const Point *pts, int n) {
	int total = 0;
	for (int i = 0; i < n; i++) {
		total += pts[i].x + pts[i].y;
	}
	return total;
}

__declspec(noinline) double scale(double v, float f, int k) { return v * f + (double)k; }

__declspec(noinline) int __stdcall std_call(int a, int b) { return a - b; }

__declspec(noinline) int __fastcall fast_call(int a, int b, int c) { return a ^ b ^ c; }

__declspec(noinline) Point make_point(int x, int y) {
	Point p = {x, y};
	return p;
}

__declspec(noinline) int use_node(Node *n, Color c) {
	if (!n) {
		fatal(3);
	}
	n->color = c;
	n->flags.mode = 5;
	return n->callback ? n->callback(n->pos[0].x, &n->pos[1]) : n->value.i;
}

static int cb(int v, Point *p) { return v + p->y; }

extern "C" int entry() {
	geo::Rect r(1, 3, 4);
	g_rect = &r;
	Box<int> b = {5};
	g_nodes[0].callback = cb;
	g_nodes[0].pos[1].y = 2;
	Point pts[2] = {{1, 2}, {3, 4}};
	int v = sum_points(pts, 2) + use_node(&g_nodes[0], Blue) + r.area() + b.get();
	v += (int)scale(1.5, 2.0f, 3) + std_call(v, 1) + fast_call(1, 2, 3) + make_point(v, 2).x;
	geo::Shape::count = v;
	return v + (int)g_handle + (int)Big::High;
}
