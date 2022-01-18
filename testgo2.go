package main

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func UmountTaks(task *PodSvc) {
	// 取消挂载
	klog.Info("ummount")
}

func MountTask(task *PodSvc) {
	// 挂载
	klog.Info("mount")
}

func dotask(f func(t *PodSvc), pod *v1.Pod, ns string, lsOptions *metav1.ListOptions) {
	podIp := pod.Status.PodIP
	klog.InfoS("podObject", "podName", pod.GetName(), "ip", podIp)
	if svcObj, err := getSVCForPod(pod, ns, lsOptions); err == nil {
		svcName := svcObj.GetName()
		klog.InfoS("found releted svc:", "pod", pod.GetName(), "svc", svcName)

		task := &PodSvc{podIP: podIp, svcName: svcName}
		f(task)
	}
}
